package recording

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const maxCrashCleanupEntries = 10_000

type fileStore struct {
	root    string
	rootDir *os.File

	closeOnce sync.Once
	closeErr  error
}

type segmentFile struct {
	directory *os.File
	file      *os.File
	temporary string
	final     string
	relative  string
}

func newFileStore(root string) (*fileStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("create recording storage: %w", ErrInvalidArgument)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve recording root: %w", ErrStorage)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create recording root: %w", ErrStorage)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve recording root symlinks: %w", ErrStorage)
	}
	if err := os.Chmod(resolved, 0o700); err != nil {
		return nil, fmt.Errorf("secure recording root: %w", ErrStorage)
	}
	fd, err := unix.Open(
		resolved,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open recording root: %w", ErrStorage)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("inspect recording root: %w", ErrStorage)
	}
	rootDir := os.NewFile(uintptr(fd), resolved)
	if rootDir == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("own recording root: %w", ErrStorage)
	}
	return &fileStore{
		root:    filepath.Clean(resolved),
		rootDir: rootDir,
	}, nil
}

func (s *fileStore) close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.rootDir != nil {
			s.closeErr = s.rootDir.Close()
			s.rootDir = nil
		}
	})
	if s.closeErr != nil {
		return fmt.Errorf("close recording root: %w", ErrStorage)
	}
	return nil
}

func (s *fileStore) createSegment(callID, segmentID string) (*segmentFile, error) {
	if s == nil || !validOpaqueID(callID) || !validOpaqueID(segmentID) {
		return nil, ErrInvalidArgument
	}
	directory, err := s.openCallDirectory(callID, true)
	if err != nil {
		return nil, err
	}
	temporary := "." + segmentID + ".ogg.part"
	final := segmentID + ".ogg"
	fd, err := unix.Openat(
		int(directory.Fd()),
		temporary,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		_ = directory.Close()
		return nil, fmt.Errorf("create partial recording: %w", ErrStorage)
	}
	file := os.NewFile(uintptr(fd), temporary)
	if file == nil {
		_ = unix.Close(fd)
		_ = directory.Close()
		return nil, fmt.Errorf("own partial recording: %w", ErrStorage)
	}
	if err := validateRegularFile(file, 0o600); err != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(directory.Fd()), temporary, 0)
		_ = directory.Close()
		return nil, err
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(directory.Fd()), temporary, 0)
		_ = directory.Close()
		return nil, fmt.Errorf("secure partial recording: %w", ErrStorage)
	}
	return &segmentFile{
		directory: directory,
		file:      file,
		temporary: temporary,
		final:     final,
		relative:  filepath.ToSlash(filepath.Join(callID, final)),
	}, nil
}

func (s *fileStore) open(relative string) (*os.File, error) {
	callID, fileName, err := parseRelativeRecording(relative)
	if err != nil {
		return nil, err
	}
	directory, err := s.openCallDirectory(callID, false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	fd, err := unix.Openat(
		int(directory.Fd()),
		fileName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if errors.Is(err, unix.ENOENT) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open recording file: %w", ErrStorage)
	}
	file := os.NewFile(uintptr(fd), fileName)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("own recording file: %w", ErrStorage)
	}
	if err := validateRegularFile(file, 0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (s *fileStore) remove(relative string) error {
	callID, fileName, err := parseRelativeRecording(relative)
	if err != nil {
		return err
	}
	directory, err := s.openCallDirectory(callID, false)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	defer directory.Close()
	var stat unix.Stat_t
	err = unix.Fstatat(
		int(directory.Fd()),
		fileName,
		&stat,
		unix.AT_SYMLINK_NOFOLLOW,
	)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o777 != 0o600 ||
		stat.Nlink != 1 {
		return fmt.Errorf("inspect recording file for removal: %w", ErrStorage)
	}
	if err := unix.Unlinkat(int(directory.Fd()), fileName, 0); err != nil {
		return fmt.Errorf("remove recording file: %w", ErrStorage)
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync recording directory after removal: %w", ErrStorage)
	}
	return nil
}

func (s *fileStore) cleanupPartialFiles() error {
	if s == nil || s.rootDir == nil {
		return ErrStorage
	}
	root, err := duplicateFile(s.rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	callEntries, err := root.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read recording root: %w", ErrStorage)
	}
	entries := 0
	for _, callEntry := range callEntries {
		entries++
		if entries > maxCrashCleanupEntries {
			return fmt.Errorf("recording cleanup entry limit exceeded: %w", ErrStorage)
		}
		var callStat unix.Stat_t
		err := unix.Fstatat(
			int(root.Fd()),
			callEntry.Name(),
			&callStat,
			unix.AT_SYMLINK_NOFOLLOW,
		)
		if err != nil ||
			callStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
			callStat.Mode&0o777 != 0o700 ||
			!validOpaqueID(callEntry.Name()) {
			return fmt.Errorf("inspect recording call directory: %w", ErrStorage)
		}
		directory, err := s.openCallDirectory(callEntry.Name(), false)
		if err != nil {
			return err
		}
		fileEntries, readErr := directory.ReadDir(-1)
		if readErr != nil {
			_ = directory.Close()
			return fmt.Errorf("read recording call directory: %w", ErrStorage)
		}
		removed := false
		for _, fileEntry := range fileEntries {
			entries++
			if entries > maxCrashCleanupEntries {
				_ = directory.Close()
				return fmt.Errorf("recording cleanup entry limit exceeded: %w", ErrStorage)
			}
			var fileStat unix.Stat_t
			err := unix.Fstatat(
				int(directory.Fd()),
				fileEntry.Name(),
				&fileStat,
				unix.AT_SYMLINK_NOFOLLOW,
			)
			if err != nil ||
				fileStat.Mode&unix.S_IFMT != unix.S_IFREG ||
				fileStat.Mode&0o777 != 0o600 ||
				fileStat.Nlink != 1 {
				_ = directory.Close()
				return fmt.Errorf(
					"inspect recording storage entry %q (mode %o, error %v): %w",
					fileEntry.Name(),
					fileStat.Mode,
					err,
					ErrStorage,
				)
			}
			name := fileEntry.Name()
			if validPartialRecordingName(name) {
				if err := unix.Unlinkat(int(directory.Fd()), name, 0); err != nil {
					_ = directory.Close()
					return fmt.Errorf("remove partial recording: %w", ErrStorage)
				}
				removed = true
				continue
			}
			if !validReadyRecordingName(name) {
				_ = directory.Close()
				return fmt.Errorf("unexpected recording storage entry: %w", ErrStorage)
			}
		}
		if removed {
			if err := directory.Sync(); err != nil {
				_ = directory.Close()
				return fmt.Errorf("sync recording cleanup: %w", ErrStorage)
			}
		}
		if err := directory.Close(); err != nil {
			return fmt.Errorf("close recording call directory: %w", ErrStorage)
		}
	}
	return nil
}

func (s *fileStore) openCallDirectory(callID string, create bool) (*os.File, error) {
	if s == nil || s.rootDir == nil || !validOpaqueID(callID) {
		return nil, ErrInvalidArgument
	}
	rootFD := int(s.rootDir.Fd())
	if create {
		err := unix.Mkdirat(rootFD, callID, 0o700)
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, fmt.Errorf("create call recording directory: %w", ErrStorage)
		}
	}
	fd, err := unix.Openat(
		rootFD,
		callID,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if errors.Is(err, unix.ENOENT) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open call recording directory: %w", ErrStorage)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil ||
		stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("inspect call recording directory: %w", ErrStorage)
	}
	if create {
		if err := unix.Fchmod(fd, 0o700); err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("secure call recording directory: %w", ErrStorage)
		}
	} else if stat.Mode&0o777 != 0o700 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("insecure call recording directory: %w", ErrStorage)
	}
	directory := os.NewFile(uintptr(fd), callID)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("own call recording directory: %w", ErrStorage)
	}
	return directory, nil
}

func (s *segmentFile) publish() (int64, error) {
	if s == nil || s.file == nil || s.directory == nil {
		return 0, ErrStorage
	}
	if err := validateRegularFile(s.file, 0o600); err != nil {
		_ = s.abort()
		return 0, err
	}
	if err := s.file.Sync(); err != nil {
		_ = s.abort()
		return 0, fmt.Errorf("sync partial recording: %w", ErrStorage)
	}
	if err := unix.Renameat2(
		int(s.directory.Fd()),
		s.temporary,
		int(s.directory.Fd()),
		s.final,
		unix.RENAME_NOREPLACE,
	); err != nil {
		_ = s.abort()
		return 0, fmt.Errorf("publish recording without overwrite: %w", ErrStorage)
	}
	if err := s.directory.Sync(); err != nil {
		_ = s.closeDescriptors()
		return 0, fmt.Errorf("sync published recording directory: %w", ErrStorage)
	}
	info, err := s.file.Stat()
	if err != nil {
		_ = s.closeDescriptors()
		return 0, fmt.Errorf("inspect published recording: %w", ErrStorage)
	}
	size := info.Size()
	if err := s.closeDescriptors(); err != nil {
		return 0, err
	}
	return size, nil
}

func (s *segmentFile) abort() error {
	if s == nil {
		return nil
	}
	var result error
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close partial recording: %w", ErrStorage))
		}
		s.file = nil
	}
	if s.directory != nil {
		err := unix.Unlinkat(int(s.directory.Fd()), s.temporary, 0)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			result = errors.Join(result, fmt.Errorf("remove partial recording: %w", ErrStorage))
		}
		if err == nil {
			if syncErr := s.directory.Sync(); syncErr != nil {
				result = errors.Join(result, fmt.Errorf("sync partial recording removal: %w", ErrStorage))
			}
		}
		if err := s.directory.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close recording directory: %w", ErrStorage))
		}
		s.directory = nil
	}
	return result
}

func (s *segmentFile) closeDescriptors() error {
	var result error
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close recording file: %w", ErrStorage))
		}
		s.file = nil
	}
	if s.directory != nil {
		if err := s.directory.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close recording directory: %w", ErrStorage))
		}
		s.directory = nil
	}
	return result
}

func parseRelativeRecording(relative string) (string, string, error) {
	relative = filepath.ToSlash(strings.TrimSpace(relative))
	parts := strings.Split(relative, "/")
	if len(parts) != 2 ||
		!validOpaqueID(parts[0]) ||
		!validReadyRecordingName(parts[1]) {
		return "", "", ErrInvalidArgument
	}
	return parts[0], parts[1], nil
}

func validReadyRecordingName(name string) bool {
	return strings.HasSuffix(name, ".ogg") &&
		validOpaqueID(strings.TrimSuffix(name, ".ogg"))
}

func validPartialRecordingName(name string) bool {
	return strings.HasPrefix(name, ".") &&
		strings.HasSuffix(name, ".ogg.part") &&
		validOpaqueID(strings.TrimSuffix(strings.TrimPrefix(name, "."), ".ogg.part"))
}

func validOpaqueID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateRegularFile(file *os.File, mode fs.FileMode) error {
	if file == nil {
		return ErrStorage
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o777 != uint32(mode.Perm()) ||
		stat.Nlink != 1 {
		return fmt.Errorf("inspect recording file: %w", ErrStorage)
	}
	return nil
}

func duplicateFile(file *os.File) (*os.File, error) {
	if file == nil {
		return nil, ErrStorage
	}
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		return nil, fmt.Errorf("duplicate recording directory: %w", ErrStorage)
	}
	unix.CloseOnExec(fd)
	duplicate := os.NewFile(uintptr(fd), file.Name())
	if duplicate == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("own duplicated recording directory: %w", ErrStorage)
	}
	return duplicate, nil
}

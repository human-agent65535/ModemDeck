# A single archive supplies exactly one bundled Opus to every native adapter.
file(GLOB_RECURSE MD_ARCHIVES "${MD_BUILD_DIR}/*.a")
set(MD_OUTPUT "${MD_BUILD_DIR}/libmd_audio_core.a")
list(REMOVE_ITEM MD_ARCHIVES "${MD_OUTPUT}")
list(SORT MD_ARCHIVES)
if(MD_APPLE)
  execute_process(COMMAND /usr/bin/libtool -static -o "${MD_OUTPUT}" ${MD_ARCHIVES}
    COMMAND_ERROR_IS_FATAL ANY)
else()
  set(MD_MRI "CREATE ${MD_OUTPUT}\n")
  foreach(archive IN LISTS MD_ARCHIVES)
    string(APPEND MD_MRI "ADDLIB ${archive}\n")
  endforeach()
  string(APPEND MD_MRI "SAVE\nEND\n")
  file(WRITE "${MD_BUILD_DIR}/merge.mri" "${MD_MRI}")
  execute_process(COMMAND "${MD_AR}" -M INPUT_FILE "${MD_BUILD_DIR}/merge.mri"
    COMMAND_ERROR_IS_FATAL ANY)
endif()

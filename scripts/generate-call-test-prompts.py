import argparse
import hashlib
import json
import os
from pathlib import Path

parser = argparse.ArgumentParser(description="Generate fixed synthetic call-test prompts; no runtime TTS dependency.")
parser.add_argument("--work-dir", type=Path, required=True, help="Ignored output/model-cache directory")
args = parser.parse_args()
ROOT = args.work_dir.resolve()
ROOT.mkdir(parents=True, exist_ok=True)
os.environ["HF_HOME"] = str(ROOT / "hf-cache")
os.environ["XDG_CACHE_HOME"] = str(ROOT / "model-cache")
os.environ["HF_HUB_DISABLE_XET"] = "1"

import numpy as np
import soundfile as sf
import torch
from huggingface_hub import hf_hub_download
from kokoro import KModel, KPipeline
from scipy.signal import resample_poly

torch.set_num_threads(4)
repository = "hexgrad/Kokoro-82M-v1.1-zh"
revision = "01e7505bd6a7a2ac4975463114c3a7650a9f7218"
def download(name):
    return hf_hub_download(repository, name, revision=revision)

weights = download("kokoro-v1_1-zh.pth")
model_hash = hashlib.sha256(Path(weights).read_bytes()).hexdigest()
assert model_hash == "b1d8410fa44dfb5c15471fd6c4225ea6b4e9ac7fa03c98e8bea47a9928476e2b"
model = KModel(repo_id=repository, config=download("config.json"), model=weights).eval()
output = ROOT / "prompts"
output.mkdir(exist_ok=True)
manifest = {"model": repository, "revision": revision, "model_sha256": model_hash,
            "license": "Apache-2.0", "sample_rate": 16000, "channels": 1, "frame_ms": 20,
            "kokoro": "0.9.4", "misaki": "0.9.4", "prompts": {}}
for language, voice, texts in [
    ("z", "zf_001", {"zh-guide": "听到提示音后，请说几句话。", "zh-playback": "现在，回放你刚才的声音。"}),
    ("a", "af_maple", {"en-guide": "After the beep, say a few words.", "en-playback": "Now, listen to your voice."}),
]:
    pipeline = KPipeline(lang_code=language, repo_id=repository, model=model)
    voice_path = download("voices/" + voice + ".pt")
    for name, content in texts.items():
        parts = [result.audio.numpy() for result in pipeline(content, voice=voice_path, speed=0.95)]
        wave = np.concatenate(parts)
        wave = resample_poly(wave, 2, 3).astype(np.float64)
        active = np.abs(wave) > 0.003
        rms = np.sqrt(np.mean(wave[active] ** 2))
        gain = min(10 ** (-20 / 20) / rms, 10 ** (-3 / 20) / np.max(np.abs(wave)))
        wave *= gain
        # Let an already activated audio route settle; the transport gates initial playback.
        wave = np.concatenate((np.zeros(4800), wave, np.zeros(3200)))
        wave = np.pad(wave, (0, (-len(wave)) % 320))
        pcm = np.round(np.clip(wave, -1, 1) * 32767).astype("<i2")
        (output / (name + ".pcm")).write_bytes(pcm.tobytes())
        sf.write(output / (name + ".wav"), pcm, 16000, subtype="PCM_16")
        manifest["prompts"][name] = {"text": content, "voice": voice, "speed": 0.95,
            "voice_sha256": hashlib.sha256(Path(voice_path).read_bytes()).hexdigest(),
            "pcm_sha256": hashlib.sha256(pcm.tobytes()).hexdigest(),
            "seconds": len(pcm) / 16000,
            "rms_dbfs": round(float(20 * np.log10(np.sqrt(np.mean(wave ** 2)))), 2),
            "peak_dbfs": round(float(20 * np.log10(np.max(np.abs(wave)))), 2)}
        print(json.dumps({name: manifest["prompts"][name]}, ensure_ascii=False), flush=True)
(output / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")

#!/usr/bin/env python3
"""Regenerate the project's own 20 ms, stereo, 440 Hz raw Opus test packet.

Requires a system libopus. Python is a fixture generation tool only.
"""
import ctypes
import ctypes.util
import math
from pathlib import Path

lib = ctypes.CDLL(ctypes.util.find_library("opus"))
lib.opus_encoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
lib.opus_encoder_create.restype = ctypes.c_void_p
lib.opus_encode.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.POINTER(ctypes.c_ubyte), ctypes.c_int]
lib.opus_encoder_destroy.argtypes = [ctypes.c_void_p]
error = ctypes.c_int()
encoder = lib.opus_encoder_create(48000, 2, 2049, ctypes.byref(error))
if error.value:
    raise RuntimeError(f"Opus encoder initialization failed: {error.value}")
try:
    pcm = (ctypes.c_int16 * 1920)()
    for i in range(960):
        pcm[2 * i] = pcm[2 * i + 1] = int(12000 * math.sin(2 * math.pi * 440 * i / 48000))
    packet = (ctypes.c_ubyte * 4000)()
    size = lib.opus_encode(encoder, pcm, 960, packet, len(packet))
    if size < 0:
        raise RuntimeError(f"Opus encoding failed: {size}")
    target = Path(__file__).resolve().parents[1] / "internal/codec/testdata/tone.opus"
    target.write_bytes(bytes(packet[:size]))
    print(f"Generated {target.name}: {size} bytes")
finally:
    lib.opus_encoder_destroy(encoder)

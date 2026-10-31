#!/usr/bin/env python3
"""Kick onsets in a take: the low band (40-140 Hz) envelope rising; inter-onset intervals against the quarter note.

Needs numpy and scipy (not the service).  python3 onsets.py take.wav 4.8 17.5 128
"""
import sys, wave, numpy as np
from scipy.signal import butter, sosfilt
path, t0, t1, bpm = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), float(sys.argv[4])
w = wave.open(path); rate = w.getframerate(); n = w.getnframes()
x = np.frombuffer(w.readframes(n), dtype=np.int16).reshape(-1, 2).mean(axis=1) / 32767.0
seg = x[int(t0 * rate):int(t1 * rate)]
low = sosfilt(butter(4, [40, 140], btype="band", fs=rate, output="sos"), seg)
env = np.abs(low)
win = int(0.005 * rate); env = np.convolve(env, np.ones(win) / win, mode="same")
thr = 0.25 * env.max(); hold = int(0.2 * rate)
onsets, i = [], 0
while i < len(env):
    if env[i] > thr:
        j = i; 
        while j < min(i + hold, len(env)) and env[j] < env[i] * 1.0: j += 1
        onsets.append(i); i += hold
    else: i += 1
ons = np.array(onsets) / rate + t0
ioi = np.diff(ons)
q = 60.0 / bpm
print(f"{len(ons)} onsets in {t0}-{t1} s; quarter {q:.4f} s; IOI mean {ioi.mean():.4f} std {ioi.std()*1000:.1f} ms, max |IOI-quarter| {np.abs(ioi-q).max()*1000:.1f} ms")
print("first onsets:", np.round(ons[:6], 3))

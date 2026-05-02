#!/usr/bin/env python3
# Copyright 2025 Substrate Systems OU
# SPDX-License-Identifier: Apache-2.0
#
# Generates the test-vector fixture consumed by
# go-engine/internal/backup/crypto/testdata/vectors.json.
#
# This is the Python REFERENCE implementation. The Go engine is the
# authoritative implementation; this script exists so we can byte-compare
# Go output against an independent third-party implementation of the
# same primitives:
#
#   - Argon2id (RFC 9106)              — argon2-cffi (libargon2)
#   - AES-256-GCM (NIST SP 800-38D)    — pyca/cryptography (OpenSSL)
#   - BIP39 mnemonic                   — python-mnemonic
#
# Determinism:
#
#   - All "randomness" inputs in the produced vectors (nonces, DEKs,
#     recovery-key entropy) are FIXED CONSTANTS embedded below. The
#     production engine uses crypto/rand for these — this script only
#     produces a stable test fixture.
#   - This script does NOT seed any global RNG. It draws nothing from
#     os.urandom. Re-running with the same script produces byte-identical
#     vectors.json.
#
# Usage:
#   python3 scripts/generate-crypto-vectors.py [output-path]
#   default output: go-engine/internal/backup/crypto/testdata/vectors.json

from __future__ import annotations

import json
import os
import struct
import sys
from dataclasses import dataclass

from argon2.low_level import Type, hash_secret_raw
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from mnemonic import Mnemonic


DEFAULT_OUT = "go-engine/internal/backup/crypto/testdata/vectors.json"

# Locked v1 KDF parameters (contract Section 2). The Go engine refuses
# anything weaker; the vectors test the locked floor.
KDF_PARAMS_V1 = {
    "algorithm": "argon2id",
    "memory": 65536,        # KiB (64 MiB)
    "iterations": 3,
    "parallelism": 4,
}

KDF_OUTPUT_BYTES = 64           # 32 serverPassword || 32 masterKey
RECOVERY_KDF_OUTPUT_BYTES = 32  # single-purpose recovery KDF
SALT_SIZE = 16
NONCE_SIZE = 12
GCM_TAG_SIZE = 16
DEK_SIZE = 32

MANIFEST_AAD_VALUE = 0xFFFFFFFF  # contract Section 3


def argon2id(password: bytes, salt: bytes, length: int) -> bytes:
    """Argon2id with the v1 locked parameters."""
    return hash_secret_raw(
        secret=password,
        salt=salt,
        time_cost=KDF_PARAMS_V1["iterations"],
        memory_cost=KDF_PARAMS_V1["memory"],
        parallelism=KDF_PARAMS_V1["parallelism"],
        hash_len=length,
        type=Type.ID,
    )


def aes_gcm_seal(key: bytes, nonce: bytes, plaintext: bytes, aad: bytes | None) -> bytes:
    """AES-256-GCM seal. Returns nonce || ciphertext || tag (Go wire format)."""
    assert len(key) == 32
    assert len(nonce) == NONCE_SIZE
    aesgcm = AESGCM(key)
    ct_and_tag = aesgcm.encrypt(nonce, plaintext, aad)
    return nonce + ct_and_tag


def aad_index(idx: int) -> bytes:
    """Match Go's binary.BigEndian.PutUint32 for chunk-index AAD."""
    return struct.pack(">I", idx & 0xFFFFFFFF)


def hexstr(b: bytes) -> str:
    return b.hex()


@dataclass
class FixedNonces:
    """Fixed nonces used for the seal-side vectors. Listed once so the
    file is the canonical source of truth for what Python is sealing."""

    chunk: list[bytes]
    manifest: list[bytes]
    dek_wrap: list[bytes]


def build_argon2id_vectors() -> list[dict]:
    cases = [
        ("hunter2-correct-horse-battery-staple", b"\x00" * SALT_SIZE),
        ("p", b"\x01" * SALT_SIZE),
        ("a much longer passphrase, with spaces and punctuation!?",
         bytes(range(SALT_SIZE))),
        ("\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e",  # non-ASCII (Japanese "Japanese")
         b"\xff" * SALT_SIZE),
        ("", bytes(range(0xF0, 0xF0 + SALT_SIZE))),
    ]
    out = []
    for pw, salt in cases:
        out.append({
            "passphrase": pw,
            "salt_hex": hexstr(salt),
            "params": KDF_PARAMS_V1,
            "output_hex": hexstr(argon2id(pw.encode("utf-8"), salt, KDF_OUTPUT_BYTES)),
        })
    return out


def build_chunk_decrypt_vectors() -> list[dict]:
    # Five chunk vectors with varying plaintext, varying chunk index, and
    # fixed-but-distinct nonces. The Go test asserts that running
    # DecryptChunk on the recorded ciphertext with the recorded key and
    # index returns the recorded plaintext.
    fixed_dek = bytes.fromhex("00112233445566778899aabbccddeeff" * 2)
    fixed_nonces = [
        b"\x10" + b"\x00" * 11,
        b"\x11" + b"\x00" * 11,
        b"\x12" + b"\x00" * 11,
        b"\x13" + b"\x00" * 11,
        b"\x14" + b"\x00" * 11,
    ]
    plaintexts = [
        b"",
        b"hello",
        b"a" * 1024,
        b"\x00" * 4096,
        bytes(range(256)) * 16,
    ]
    indices = [0, 1, 7, 1023, 0xFFFFFFFE]
    assert len(fixed_nonces) == len(plaintexts) == len(indices) == 5

    out = []
    for nonce, pt, idx in zip(fixed_nonces, plaintexts, indices):
        blob = aes_gcm_seal(fixed_dek, nonce, pt, aad_index(idx))
        out.append({
            "key_hex": hexstr(fixed_dek),
            "chunk_index": idx,
            "plaintext_hex": hexstr(pt),
            "blob_hex": hexstr(blob),
        })
    return out


def build_dek_unwrap_vectors() -> list[dict]:
    fixed_master = bytes.fromhex("ffeeddccbbaa99887766554433221100" * 2)
    fixed_nonces = [
        b"\x20" + b"\x00" * 11,
        b"\x21" + b"\x00" * 11,
        b"\x22" + b"\x00" * 11,
    ]
    deks = [
        b"\xaa" * DEK_SIZE,
        bytes(range(DEK_SIZE)),
        bytes(reversed(range(DEK_SIZE))),
    ]
    out = []
    for nonce, dek in zip(fixed_nonces, deks):
        blob = aes_gcm_seal(fixed_master, nonce, dek, None)  # WrapDEK uses no AAD
        out.append({
            "master_key_hex": hexstr(fixed_master),
            "wrapped_hex": hexstr(blob),
            "dek_hex": hexstr(dek),
        })
    return out


def build_manifest_decrypt_vectors() -> list[dict]:
    fixed_dek = bytes.fromhex("aabbccddeeff00112233445566778899" * 2)
    fixed_nonces = [
        b"\x30" + b"\x00" * 11,
        b"\x31" + b"\x00" * 11,
        b"\x32" + b"\x00" * 11,
    ]
    manifests_json = [
        b'{"envelopeVersion":1,"chunkCount":0}',
        b'{"envelopeVersion":1,"chunkCount":1,"chunks":[{"index":0}]}',
        b'{"envelopeVersion":1,"chunkCount":3}',
    ]
    out = []
    for nonce, mj in zip(fixed_nonces, manifests_json):
        blob = aes_gcm_seal(fixed_dek, nonce, mj, aad_index(MANIFEST_AAD_VALUE))
        out.append({
            "key_hex": hexstr(fixed_dek),
            "blob_hex": hexstr(blob),
            "manifest_json_hex": hexstr(mj),
        })
    return out


def build_recovery_vectors() -> list[dict]:
    # Three recovery vectors: fixed entropy → 24-word BIP39 mnemonic,
    # then KDF-derive with a fixed salt and assert byte-equality.
    fixed_entropies = [
        b"\x00" * 32,
        b"\xff" * 32,
        bytes(range(32)),
    ]
    fixed_salts = [
        b"\x00" * SALT_SIZE,
        b"\x55" * SALT_SIZE,
        bytes(range(SALT_SIZE)),
    ]
    m = Mnemonic("english")
    out = []
    for entropy, salt in zip(fixed_entropies, fixed_salts):
        phrase = m.to_mnemonic(entropy)
        derived = argon2id(entropy, salt, RECOVERY_KDF_OUTPUT_BYTES)
        verifier = argon2id(entropy, salt, RECOVERY_KDF_OUTPUT_BYTES)  # same params, same salt
        out.append({
            "entropy_hex": hexstr(entropy),
            "phrase": phrase,
            "salt_hex": hexstr(salt),
            "params": KDF_PARAMS_V1,
            "derived_recovery_key_hex": hexstr(derived),
            "verifier_hex": hexstr(verifier),
        })
    return out


def main(out_path: str) -> None:
    payload = {
        "schema_version": 1,
        "_note": (
            "Generated by scripts/generate-crypto-vectors.py. The Go "
            "test suite asserts byte-equality against this fixture. "
            "DO NOT edit by hand — re-run the script."
        ),
        "argon2id": build_argon2id_vectors(),
        "chunk_decrypt": build_chunk_decrypt_vectors(),
        "dek_unwrap": build_dek_unwrap_vectors(),
        "manifest_decrypt": build_manifest_decrypt_vectors(),
        "recovery": build_recovery_vectors(),
    }

    out_path = os.path.abspath(out_path)
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, "w", encoding="utf-8", newline="\n") as f:
        json.dump(payload, f, indent=2, sort_keys=False)
        f.write("\n")
    print(f"wrote {out_path}")


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_OUT
    main(out)

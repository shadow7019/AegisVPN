#!/usr/bin/env python3
"""
macho-merge.py — create a Mach-O universal (fat) binary from multiple
single-architecture slices, without needing Apple's `lipo`.

Pure stdlib implementation of the Mach-O fat file format:

    struct fat_header { uint32 magic(0xCAFEBABE); uint32 nfat_arch; };
    struct fat_arch {
        uint32 cputype; uint32 cpusubtype;
        uint32 offset;  uint32 size;
        uint32 align;   // power-of-two byte alignment
    };  // all fields big-endian

Usage:
    python3 macho-merge.py <output> <slice1> [<slice2> ...]
"""

import struct
import sys
from pathlib import Path

MH_MAGIC = 0xCAFEBABE
CPU_ARCH_ABI64 = 0x01000000

# cputype values for 64-bit slices
CPU_TYPE_X86_64 = CPU_ARCH_ABI64 | 0x00000007
CPU_TYPE_ARM64 = CPU_ARCH_ABI64 | 0x0000000C

SLICE_ALIGN = 1 << 14  # 16 KiB — required by arm64, safe for x86_64


def macho_cputype(data: bytes):
    """Return the fat_arch cputype for a Mach-O slice, or raise."""
    if len(data) < 32:
        raise ValueError("slice too small to be a Mach-O")
    magic = struct.unpack("<I", data[:4])[0]
    if magic == 0xFEEDFACF:  # MH_MAGIC_64, little-endian
        cputype = struct.unpack("<I", data[4:8])[0]
    elif magic == 0xCFFAEDFE:  # MH_MAGIC_64, big-endian
        cputype = struct.unpack(">I", data[4:8])[0]
    elif magic == 0xFEEDFACE:  # 32-bit, little-endian
        cputype = struct.unpack("<I", data[4:8])[0]
    elif magic == 0xCEFAEDFE:  # 32-bit, big-endian
        cputype = struct.unpack(">I", data[4:8])[0]
    else:
        raise ValueError(f"not a Mach-O (magic 0x{struct.unpack('>I', data[:4])[0]:08x})")
    return cputype


def align_up(n: int, a: int = SLICE_ALIGN) -> int:
    return (n + a - 1) & ~(a - 1)


def merge(output: str, slices: list) -> None:
    if not 1 <= len(slices) <= 8:
        raise ValueError("need 1..8 slices")

    entries = []
    payloads = []
    offset = align_up(8 + 20 * len(slices))
    seen = set()
    for path in slices:
        data = Path(path).read_bytes()
        cputype = macho_cputype(data)
        if cputype in seen:
            raise ValueError(f"duplicate architecture cputype=0x{cputype:08x}: {path}")
        seen.add(cputype)
        start = align_up(offset)
        entries.append((cputype, 0, start, len(data), 14))
        payloads.append((start, data))
        offset = start + len(data)

    out = bytearray(offset)
    struct.pack_into(">II", out, 0, MH_MAGIC, len(entries))
    for i, (cputype, cpusub, start, size, align) in enumerate(entries):
        struct.pack_into(">IIIII", out, 8 + i * 20, cputype, cpusub, start, size, align)
    for start, data in payloads:
        out[start : start + len(data)] = data

    Path(output).write_bytes(bytes(out))
    print(f"wrote {output} ({len(out)} bytes, {len(entries)} archs: "
          + ", ".join(f"0x{c:08x}" for c, *_ in entries) + ")")


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print(__doc__)
        sys.exit(1)
    merge(sys.argv[1], sys.argv[2:])

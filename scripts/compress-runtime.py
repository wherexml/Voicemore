#!/usr/bin/env python3
"""Losslessly transcode a gzip runtime to bzip2 and verify the entire tar stream."""
import bz2
import gzip
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import os


def compress(source, destination):
    source, destination = Path(source), Path(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    before = hashlib.sha256()
    fd, temporary = tempfile.mkstemp(dir=destination.parent, suffix='.bz2')
    os.close(fd)
    try:
        with gzip.open(source, 'rb') as src, bz2.open(temporary, 'wb', compresslevel=9) as dst:
            for chunk in iter(lambda: src.read(1024 * 1024), b''):
                before.update(chunk)
                dst.write(chunk)
        after = hashlib.sha256()
        with bz2.open(temporary, 'rb') as src:
            for chunk in iter(lambda: src.read(1024 * 1024), b''):
                after.update(chunk)
        if before.digest() != after.digest():
            raise RuntimeError('Runtime tar stream changed during compression')
        if Path(temporary).stat().st_size >= source.stat().st_size:
            raise RuntimeError('Compressed runtime is not smaller')
        os.replace(temporary, destination)
        return dict(before=source.stat().st_size, after=destination.stat().st_size,
                    tar_sha256=before.hexdigest())
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


if __name__ == '__main__':
    print(json.dumps(compress(*sys.argv[1:]), indent=2))

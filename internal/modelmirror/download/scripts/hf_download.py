# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Run the pinned HF CLI with only local-dir download locks on Pod-local disk.

This is not a distributed lock: the controller must serialize download Jobs and
Pods for this mirror. Payloads, metadata and incomplete files remain on the PVC.
"""

import hashlib
import os
import sys
from contextlib import contextmanager
from pathlib import Path

import filelock
import huggingface_hub
from filelock import FileLock
from huggingface_hub import utils
from huggingface_hub.utils import _fixes

SUPPORTED_HF_VERSION = "2.3.0"
SUPPORTED_FILELOCK_VERSION = "4.3.1"


def install_local_locks(model_dir: Path, lock_dir: Path) -> None:
    if (
        huggingface_hub.__version__ != SUPPORTED_HF_VERSION
        or filelock.__version__ != SUPPORTED_FILELOCK_VERSION
    ):
        raise RuntimeError("Revalidate local download locks before changing the HF pin")

    cache_dir = Path(os.path.abspath(model_dir)) / ".cache" / "huggingface"
    lock_dir.mkdir(parents=True, exist_ok=True)
    original = _fixes.WeakFileLock

    @contextmanager
    def local_file_lock(lock_file, *, timeout=None):
        path = Path(os.path.abspath(lock_file))
        if path.is_relative_to(cache_dir) and path.suffix == ".lock":
            # Map before FileLock construction: even its filesystem probes must
            # stay off BlobFuse. Never unlink locks or fall back to SoftFileLock:
            # both can break mutual exclusion between waiting local workers.
            digest = hashlib.sha256(os.fsencode(path)).hexdigest()
            lock = FileLock(
                lock_dir / (digest + ".lock"),
                mode=0o664,
                fallback_to_soft=False,
                preserve_lock_file=True,
            )
            # Own a strong reference for the entire context. None means wait
            # indefinitely; zero is nonblocking; positive values bound the wait.
            # Never delegate this path to HF's WeakFileLock: its error cleanup
            # can unlink the original PVC path, even after acquisition fails.
            try:
                lock.acquire(timeout=-1 if timeout is None else timeout)
                yield lock
            finally:
                lock.release()
        else:
            with original(lock_file, timeout=timeout) as lock:
                yield lock

    # HF imports WeakFileLock into multiple modules. Replace already imported
    # aliases as well as the exports used by modules loaded later by the CLI.
    for name, module in list(sys.modules.items()):
        if (
            name == "huggingface_hub" or name.startswith("huggingface_hub.")
        ) and getattr(module, "WeakFileLock", None) is original:
            module.WeakFileLock = local_file_lock
    _fixes.WeakFileLock = utils.WeakFileLock = local_file_lock

    # HF 2.3.0's _huggingface_dir explicitly unlinks .gitignore.lock outside
    # WeakFileLock. Initialize this tiny metadata file first to avoid that path;
    # do not redirect metadata or the large *.incomplete files to local disk.
    cache_dir.mkdir(parents=True, exist_ok=True)
    with local_file_lock(cache_dir / ".gitignore.lock"):
        gitignore = cache_dir / ".gitignore"
        if not gitignore.exists():
            gitignore.write_text("*")


def main():
    install_local_locks(Path("/models") / os.environ["MODEL_ID"], Path("/hf-locks"))
    from huggingface_hub.cli.hf import main as hf_main

    hf_main()


if __name__ == "__main__":
    main()

# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import sys
import os
import nest_asyncio

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../..')))
os.environ["CUDA_VISIBLE_DEVICES"] = "-1" # Force CPU-only execution for testing
os.environ["OMP_NUM_THREADS"] = "1" # Force single-threaded for testing to prevent segfault while loading embedding model
os.environ["MKL_NUM_THREADS"] = "1"  # Force MKL to use a single thread

# Apply nest_asyncio to allow nested event loops
nest_asyncio.apply()

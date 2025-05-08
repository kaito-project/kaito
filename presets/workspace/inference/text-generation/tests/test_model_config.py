# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import importlib
import sys
from pathlib import Path

import pytest

# Get the parent directory of the current file
parent_dir = str(Path(__file__).resolve().parent.parent)
# Add the parent directory to sys.path
sys.path.append(parent_dir)

@pytest.fixture(params=[
    {"pipeline": "text-generation", "model_path": "stanford-crfm/alias-gpt2-small-x21"},
    {"pipeline": "conversational", "model_path": "stanford-crfm/alias-gpt2-small-x21"},
])
def configured_model_config(request):
    original_argv = sys.argv.copy()

    sys.argv = [
        'program_name',
        '--pipeline', request.param['pipeline'],
        '--pretrained_model_name_or_path', request.param['model_path'],
        '--allow_remote_files', 'True'
    ]

    import inference_api
    importlib.reload(inference_api)
    from inference_api import ModelConfig

    # Create and configure the ModelConfig instance
    model_config = ModelConfig(
        pipeline=request.param['pipeline'],
        pretrained_model_name_or_path=request.param['model_path'],
    )

    yield model_config

    # Restore the original sys.argv after the test is done
    sys.argv = original_argv

def test_process_additional_args(configured_model_config):
    config = configured_model_config

    # Simulate additional command-line arguments
    additional_args = [
        "--new_arg1", "value1",
        "--new_arg2",
        "--new_arg3", "value3",
        "--flag_arg"
    ]

    # Process the additional arguments
    config.process_additional_args(additional_args)

    # Assertions to verify that additional arguments were processed correctly
    assert getattr(config, "new_arg1", None) == "value1"
    assert getattr(config, "new_arg2", None) is True
    assert getattr(config, "new_arg3", None) == "value3"
    assert getattr(config, "flag_arg", None) is True

# Test case for ignoring arguments prefixed with '--' when expecting a value
def test_ignore_double_dash_arguments(configured_model_config):
    config = configured_model_config
    additional_args = [
        "--new_arg1", "--new_arg2",
        "--new_arg3", "correct_value"
    ]

    config.process_additional_args(additional_args)

    # new_arg1 should be set to True since its value is incorrectly prefixed with '--'
    assert getattr(config, "new_arg1", None) is True
    assert getattr(config, "new_arg2", None) is True
    assert getattr(config, "new_arg3", None) == "correct_value"

# Test case to verify handling unsupported pipeline values
def test_unsupported_pipeline_raises_value_error(configured_model_config):
    with pytest.raises(ValueError) as excinfo:
        from inference_api import ModelConfig
        ModelConfig(pipeline="unsupported_pipeline")
    assert "Unsupported pipeline" in str(excinfo.value)

# Test case for validating torch_dtype
def test_invalid_torch_dtype_raises_value_error(configured_model_config):
    with pytest.raises(ValueError) as excinfo:
        from inference_api import ModelConfig
        ModelConfig(pipeline="text-generation", torch_dtype="unsupported_dtype")
    assert "Invalid torch dtype" in str(excinfo.value)
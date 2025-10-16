import os


def get_required_env_var(var_name: str) -> str:
    value = os.getenv(var_name)
    if not value:
        raise Exception(f"Required environment variable '{var_name}' is not set.")
    return value

# Use defaults for testing, but require them at runtime
NAMESPACE = get_required_env_var("NAMESPACE")
AUTOINDEXER_NAME = get_required_env_var("AUTOINDEXER_NAME")
ACCESS_SECRET = os.getenv("ACCESS_SECRET")
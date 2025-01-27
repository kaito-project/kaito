import json
import os
import shutil
import subprocess
import uuid
from pathlib import Path

import yaml


def read_yaml(file_path):
    try:
        with open(file_path, 'r') as file:
            data = yaml.safe_load(file)
            return data
    except (IOError, yaml.YAMLError) as e:
        print(f"Error reading {file_path}: {e}")
        return None

supp_models_yaml = 'presets/workspace/models/supported_models.yaml'
YAML_PR = read_yaml(supp_models_yaml)
# Format: {falcon-7b : {model_name:falcon-7b, type:text-generation, version: #, tag: #}}
MODELS = {model['name']: model for model in YAML_PR['models']}
KAITO_REPO_URL = "https://github.com/kaito-project/kaito.git"

def set_multiline_output(name, value):
    with open(os.environ['GITHUB_OUTPUT'], 'a') as fh:
        delimiter = uuid.uuid1()
        print(f'{name}<<{delimiter}', file=fh)
        print(value, file=fh)
        print(delimiter, file=fh)

def create_matrix(models_list):
    """Create GitHub Matrix"""
    matrix = [MODELS[model] for model in models_list]
    return json.dumps(matrix)

def run_command(command):
    """Execute a shell command and return the output."""
    try:
        process = subprocess.Popen(command, stdout=subprocess.PIPE, 
                                   stderr=subprocess.PIPE, shell=True)
        output, error = process.communicate()
        if process.returncode != 0:
            print(f"Error: {error.decode('utf-8')}")
            return None
        return output.decode('utf-8').strip()
    except Exception as e:
        print(f"An error occurred: {e}")
        return None

def get_yaml_from_branch(branch, file_path):
    """Read YAML from a branch"""
    subprocess.run(['git', 'fetch', 'origin', branch], check=True)
    subprocess.run(['git', 'checkout', 'origin/' + branch], check=True)
    return read_yaml(file_path)

def detect_changes_in_yaml(yaml_main, yaml_pr): 
    """Detecting relevant changes in support_models.yaml"""
    yaml_main, yaml_pr = yaml_main['models'], yaml_pr['models']

    models_to_build = []
    for model_pr in yaml_pr:
        # Searches for matching models
        model_main = next((m for m in yaml_main if m['name'] == model_pr['name']), None)
        # New Model
        if not model_main:
            print("New Model: ", model_pr['name'])
            models_to_build.append(model_pr['name'])
        # Model Version Update
        elif model_pr.get('version') != model_main.get('version'):
            print("Updated Version of Model: ", model_pr['name'])
            models_to_build.append(model_pr['name'])
        # Model Tag Update
        elif model_pr.get('tag') != model_main.get('tag'):
            print("Update Tag of Model: ", model_pr['name'])
            models_to_build.append(model_pr['name'])
    return models_to_build

def models_to_build(files_changed):
    """Models to build based on changed files."""
    models, seen_model_types = set(), set()
    if supp_models_yaml in files_changed:
        yaml_main = get_yaml_from_branch('main', supp_models_yaml)
        models.update(detect_changes_in_yaml(yaml_main, YAML_PR))
    for model, model_info in MODELS.items():
        if model_info["type"] not in seen_model_types: 
            if any(file.startswith(f'presets/workspace/inference/{model_info["type"]}') for file in files_changed):
                models.add(model)
                seen_model_types.add(model_info["type"])
    return list(models)

def check_modified_models(pr_branch):
    """Check for modified models in the repository."""
    repo_dir = Path.cwd() / "repo"

    if repo_dir.exists():
        shutil.rmtree(repo_dir)

    run_command(f"git clone {KAITO_REPO_URL} {repo_dir}")
    os.chdir(repo_dir)

    run_command("git checkout --detach")
    run_command("git fetch origin main:main")
    run_command(f"git fetch origin {pr_branch}:{pr_branch}")
    run_command(f"git checkout {pr_branch}")

    files = run_command("git diff --name-only origin/main") # Returns each file on newline
    files = files.split("\n")
    os.chdir(Path.cwd().parent)

    modified_models = models_to_build(files)
    
    print("Modified Models (Images to build): ", modified_models)

    return modified_models

def main():
    pr_branch = os.environ.get("PR_BRANCH", "main") # If not specified default to 'main'
    force_run_all = os.environ.get("FORCE_RUN_ALL", "false") # If not specified default to False
    force_run_all_phi = os.environ.get("FORCE_RUN_ALL_PHI", "false") # If not specified default to False
    force_run_all_public = os.environ.get("FORCE_RUN_ALL_PUBLIC", "false") # If not specified default to False

    affected_models = []
    if force_run_all != "false":
        affected_models = [model['name'] for model in YAML_PR['models']]
    elif force_run_all_public != "false": 
        affected_models = [model['name'] for model in YAML_PR['models'] if "llama" not in model['name']]
    elif force_run_all_phi != "false":
        affected_models = [model['name'] for model in YAML_PR['models'] if 'phi-3' in model['name']]
    else:
        # Logic to determine affected models
        # Example: affected_models = ['model1', 'model2', 'model3']
        affected_models = check_modified_models(pr_branch)

    # Convert the list of models into JSON matrix format
    matrix = create_matrix(affected_models)
    print("Create Matrix: ", matrix)

    # Set the matrix as an output for the GitHub Actions workflow
    set_multiline_output('matrix', matrix)

if __name__ == "__main__":
    main()


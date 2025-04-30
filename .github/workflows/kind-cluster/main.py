# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import os
import random
import shutil
import string
import subprocess
import time
from pathlib import Path

KAITO_REPO_URL = "https://github.com/kaito-project/kaito.git"
WEIGHTS_FOLDER = os.environ.get("WEIGHTS_DIR", None)


def get_weights_path(model_name):
    return f"{WEIGHTS_FOLDER}/{model_name}/weights"


def get_dockerfile_path(model_runtime):
    return f"/kaito/docker/presets/models/{model_runtime}/Dockerfile"


def generate_unique_id():
    """Generate a unique identifier for a job."""
    timestamp = int(time.time())
    random_str = ''.join(random.choices(string.ascii_lowercase + string.digits, k=6))
    return f"{timestamp}-{random_str}"


def run_command(command):
    """Execute a shell command and return the output."""
    try:
        process = subprocess.Popen(command, stdout=subprocess.PIPE, shell=True)
        output, error = process.communicate()
        if error:
            print(f"Error: {error}")
        return output.decode('utf-8').strip()
    except Exception as e:
        print(f"An error occurred: {e}")
        return None


def get_kubectl_path():
    """Get the full path to kubectl."""
    kubectl_path = "/usr/local/bin/kubectl"
    if not os.path.exists(kubectl_path):
        raise FileNotFoundError("kubectl not found at /usr/local/bin/kubectl")
    return kubectl_path


def get_model_git_info(model_version, hf_username, hf_token):
    """Get model Git Repo link and commit ID"""
    url_parts = model_version.split('/')
    # Add Auth to URL
    if hf_username and hf_token:
        url_parts[2] = f'{hf_username}:{hf_token}@{url_parts[2]}'
    model_url = '/'.join(url_parts[:-2])
    commit_id = url_parts[-1]
    return model_url, commit_id


def update_model(model_name, model_commit):
    """Update the model to a specific commit, including LFS files."""
    weights_path = get_weights_path(model_name)
    git_files_path = os.path.join(weights_path, "..", "git_files", ".git")
    start_dir = os.getcwd()
    try:
        # Change to weights directory
        os.chdir(weights_path)
        # Allow current runner access to git dir
        run_command(f"git config --global --add safe.directory {weights_path}")
        run_command(f"git config --global --add safe.directory {git_files_path}")

        run_command(f"git --git-dir={git_files_path} checkout main")
        run_command(f"git --git-dir={git_files_path} pull origin main")
        # Checkout to the specific commit
        run_command(f"git --git-dir={git_files_path} checkout {model_commit}")
        # Pull LFS files for the checked-out commit
        run_command(f"git --git-dir={git_files_path} lfs pull")
        # Remove the cached .git/lfs directory to save space (Optimization)
        # run_command(f"rm -rf {os.path.join(git_files_path, 'lfs')}")
    except Exception as e:
        print(f"An error occurred: {e}")
        exit(1)
    finally:
        # Change back to the original directory
        os.chdir(start_dir)


def download_new_model(model_name, model_url):
    """Given URL download new model."""
    weights_path = get_weights_path(model_name)
    git_files_path = os.path.join(weights_path, "..", "git_files")  # Path for git_files directory
    print("Weights Path:", weights_path)
    print("Git Files Path:", git_files_path)

    start_dir = os.getcwd()

    if not os.path.exists(weights_path) and model_url:
        try:
            os.makedirs(weights_path, exist_ok=True)
            os.chdir(weights_path)
            run_command(f"git clone {model_url} .")

            # Create git_files directory and move .git there
            os.makedirs(git_files_path, exist_ok=True)
            shutil.move(os.path.join(weights_path, ".git"), git_files_path)
        except Exception as e:
            print(f"An error occurred: {e}")
            exit(1)
        finally:
            os.chdir(start_dir)


def main():
    pr_branch = os.environ.get("PR_BRANCH", "main")
    model_name = os.environ.get("MODEL_NAME", None)
    image_name = os.environ.get("IMAGE_NAME", model_name)
    model_version = os.environ.get("MODEL_VERSION", None)
    model_runtime = os.environ.get("MODEL_RUNTIME", None)
    model_type = os.environ.get("MODEL_TYPE", None)
    model_tag = os.environ.get("MODEL_TAG", None)
    hf_username = os.environ.get("HF_USERNAME", None)
    hf_token = os.environ.get("HF_TOKEN", None)

    # No need to pull model weights if we are building the "base" image.
    # it is for preset models that require downloading the model weights
    # from HuggingFace at runtime.
    if model_version and model_name != "base":
        model_url, model_commit = get_model_git_info(model_version, hf_username, hf_token)
        download_new_model(model_name, model_url)
        update_model(model_name, model_commit)

        weights = run_command(f"ls {WEIGHTS_FOLDER}")
        print("Models Present:", weights)

        output = run_command(f"ls {get_weights_path(model_name)}")
        print("Model Weights:", output)

    clone_and_checkout_pr_branch(pr_branch)

    job_names = []

    unique_id = generate_unique_id()
    job_name = f"{model_name}-{unique_id}"
    job_yaml = populate_job_template(image_name, model_name, model_type, model_runtime, model_tag, job_name, os.environ)
    write_job_file(job_yaml, job_name)

    kubectl_path = get_kubectl_path()
    run_command(f"{kubectl_path} apply -f {job_name}-job.yaml")
    job_names.append(job_name)

    if not wait_for_jobs_to_complete(job_names):
        exit(1)  # Exit with an error code if any job failed


def write_job_file(job_yaml, job_name):
    """Write the job yaml to a file."""
    if job_yaml:
        with open(f"{job_name}-job.yaml", "w") as file:
            file.write(job_yaml)


def clone_and_checkout_pr_branch(pr_branch):
    """Clone and checkout PR Branch."""
    repo_dir = Path.cwd() / "repo"

    if repo_dir.exists():
        shutil.rmtree(repo_dir)

    run_command(f"git clone {KAITO_REPO_URL} {repo_dir}")
    os.chdir(repo_dir)

    run_command("git checkout --detach")
    run_command("git fetch origin main:main")
    run_command(f"git fetch origin {pr_branch}:{pr_branch}")
    run_command(f"git checkout {pr_branch}")

    os.chdir(Path.cwd().parent)


def populate_job_template(image_name, model_name, model_type, model_runtime, model_tag, job_name, env_vars):
    """Populate the job template with provided values."""
    try:
        docker_job_template = Path.cwd() / "repo/.github/workflows/kind-cluster/docker-job-template.yaml"
        with open(docker_job_template, "r") as file:
            job_template = file.read()

        replacements = {
            "{{JOB_ID}}": f"{job_name}",
            "{{IMAGE_NAME}}": f"{image_name}",
            "{{ACR_NAME}}": env_vars["ACR_NAME"],
            "{{ACR_USERNAME}}": env_vars["ACR_USERNAME"],
            "{{ACR_PASSWORD}}": env_vars["ACR_PASSWORD"],
            "{{PR_BRANCH}}": env_vars["PR_BRANCH"],
            "{{HOST_WEIGHTS_PATH}}": get_weights_path(model_name),
            "{{MODEL_TYPE}}": model_type,
            "{{DOCKERFILE_PATH}}": get_dockerfile_path(model_runtime),
            "{{VERSION}}": model_tag,
            "{{BUILD_TARGET}}": model_name if model_name == "base" else "model",
        }

        for key, value in replacements.items():
            job_template = job_template.replace(key, value)

        return job_template
    except Exception as e:
        print(f"An error occurred while populating job template: {e}")
        return None


def log_job_info(job_name):
    """Log information about our Job's pod for debugging."""
    # Describe the job
    # command_describe_job = f"kubectl describe job {job_name}"
    # job_desc = run_command(command_describe_job)
    # print(f"Job Description: \n{job_desc}")
    # print("===============================\n")
    # Find the pod(s) associated with the job
    command_find_pods = f"kubectl get pods --selector=job-name=docker-build-job-{job_name} -o jsonpath='{{.items[*].metadata.name}}'"
    pod_names = run_command(command_find_pods)
    if pod_names:
        for pod_name in pod_names.split():
            print(f"Logging info for pod: {pod_name}")
            # Log pod description for status, events, etc.
            command_describe_pod = f"kubectl describe pod {pod_name}"
            pod_description = run_command(command_describe_pod)
            print(f"Pod Description: \n{pod_description}")

            # Log the last 100 lines of the pod's logs, adjust as necessary
            command_logs = f"kubectl logs {pod_name} --tail=100"
            pod_logs = run_command(command_logs)
            print(f"Pod Logs: \n{pod_logs}")
    else:
        print(f"No pods found for job {job_name}.")


def check_job_status(job_name, iteration):
    """Check the status of a Kubernetes job."""
    # Every 2.5 minutes log job information
    if iteration % 5 == 0:
        log_job_info(job_name)
    # Query for the specific fields 'succeeded' and 'failed' in the job's status
    command_succeeded = f"kubectl get job docker-build-job-{job_name} -o jsonpath='{{.status.succeeded}}'"
    command_failed = f"kubectl get job docker-build-job-{job_name} -o jsonpath='{{.status.failed}}'"

    succeeded = run_command(command_succeeded)
    failed = run_command(command_failed)

    if succeeded and int(succeeded) > 0:
        return "succeeded"
    elif failed and int(failed) > 0:
        return "failed"
    else:
        return "running"


def wait_for_jobs_to_complete(job_names, timeout=21600):
    """Wait for all jobs to complete with a timeout."""
    iteration = 0
    start_time = time.time()
    while time.time() - start_time < timeout:
        all_completed = True
        for job_name in job_names:
            print("Check Job Status: ", job_name)
            status = check_job_status(job_name, iteration)
            if status != "succeeded":
                all_completed = False
                if status == "failed":
                    print(f"Job {job_name} failed.")
                    return False
            time.sleep(5)  # Wait for 5 sec between requests - prevents connection errors
        if all_completed:
            print("All jobs completed successfully.")
            return True
        time.sleep(30)  # Wait for 30 seconds before checking again
        iteration += 1
    print("Timeout waiting for jobs to complete.")
    return False


if __name__ == "__main__":
    main()

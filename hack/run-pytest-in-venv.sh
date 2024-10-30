#!/usr/bin/env bash

set -e

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <test_dir> <requirements.txt>"
    exit 1
fi

PROJECT_DIR=$(dirname "$(dirname "$(realpath "$0")")")

TEST_DIR="$PROJECT_DIR/$1"
REQUIREMENTS="$PROJECT_DIR/$2"
VENV_DIR=$(mktemp -d)

cleanup() {
    rm -rf "$VENV_DIR"
}
trap cleanup EXIT

cd $VENV_DIR
printf "Creating virtual environment in %s\n" "$VENV_DIR"
python3 -m virtualenv --system-site-packages venv
source "$VENV_DIR/venv/bin/activate"
if [ "$?" -ne 0 ]; then
    printf "Failed to activate virtual environment\n"
    exit 1
fi

printf "Installing requirements from %s\n" "$REQUIREMENTS"
pip install -r "$REQUIREMENTS" > "$VENV_DIR/pip.log"
if [ "$?" -ne 0 ]; then
    cat "$VENV_DIR/pip.log"
    exit 1
fi

printf "Running tests in %s\n" "$TEST_DIR"
pytest -o log_cli=true -o log_cli_level=INFO "$TEST_DIR"

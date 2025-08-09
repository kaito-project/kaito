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

import csv
import random
import time
from datetime import datetime

import requests


def is_file_empty(filename):
    try:
        return not bool(len(open(filename).readline()))
    except FileNotFoundError:
        return True


def load_search_terms_from_csv(filename):
    with open(filename, newline="") as file:
        reader = csv.DictReader(file)
        return [row["search_terms"] for row in reader]


# Load search terms and shuffle them for randomness
search_terms = load_search_terms_from_csv("../common-gpt-questions.csv")
random.shuffle(search_terms)

# Constants
URL = "http://20.241.194.198/chat"  # Replace with service URL
input_payload = {
    "input_data": {
        "input_string": [[{"role": "user", "content": ""}]],
    },
    "parameters": {"temperature": 0.6, "top_p": 0.9, "max_gen_len": 64},
}

NUM_REQUESTS = min(
    1000, len(search_terms)
)  # Cannot exceed the number of questions available

# Generate a unique run_id based on the current timestamp
run_id = int(time.time())

# List to store latencies
latencies = []

# Open the CSV for writing
with open("gpt-requests.csv", "a", newline="") as file:
    writer = csv.writer(file)

    # If the file is empty, write the header
    if is_file_empty("gpt-requests.csv"):
        writer.writerow(["run_id", "request_id", "request_num", "latency", "timestamp"])

    for i in range(NUM_REQUESTS):
        question = search_terms.pop()  # Get a random question without replacement
        print("Question Asked: ", question)
        input_payload["input_data"]["input_string"][0][0]["content"] = question

        start_time = time.time()

        response = requests.post(URL, json=input_payload)
        print("Response Status:", response.status_code)
        print("Response Body:", response.text)
        # Get the date/time for this request
        request_date = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

        if response.status_code == 200:
            elapsed_time = (time.time() - start_time) * 1000
            print(f"Request #{i + 1} elapsed time", elapsed_time)
            latencies.append(elapsed_time)
            request_id = f"{run_id}-{i + 1}"
            writer.writerow([run_id, request_id, i + 1, elapsed_time, request_date])
        else:
            print(
                f"Request #{i + 1} failed with status code {response.status_code}. Error: {response.text}"
            )

# Calculate statistics
average_latency = sum(latencies) / len(latencies)
max_latency = max(latencies)
min_latency = min(latencies)

print(f"Average latency: {average_latency:.2f} ms")
print(f"Max latency: {max_latency:.2f} ms")
print(f"Min latency: {min_latency:.2f} ms")

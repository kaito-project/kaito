# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import os
from urllib.parse import urljoin

import chainlit as cl
import requests

URL = os.environ.get('WORKSPACE_SERVICE_URL')
@cl.step
def inference(prompt):
    # Endpoint URL
    data = {
        "prompt": prompt,
        "return_full_text": False,
        "clean_up_tokenization_spaces": True,
        "generate_kwargs": {
            "max_length": 256,
            "min_length": 0,
            "do_sample": True,
            "top_k": 10,
            "early_stopping": False,
            "num_beams": 1,
            "temperature": 1.0,
            "top_p": 1,
            "typical_p": 1,
            "repetition_penalty": 1
        }
    }

    response = requests.post(urljoin(URL, "chat"), json=data)

    if response.status_code == 200:
        response_data = response.json()
        return response_data.get("Result", "No result found")
    else:
        return f"Error: Received response code {response.status_code}"

@cl.on_message
async def main(message: cl.Message):
    """
    This function is called every time a user inputs a message in the UI.
    It sends back an intermediate response from inference, followed by the final answer.

    Args:
        message: The user's message.

    Returns:
        None.
    """

    # Call inference
    response = inference(message.content)

    # Send the final answer
    await cl.Message(content=response).send()

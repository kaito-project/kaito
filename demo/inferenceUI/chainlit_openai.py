# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import os
from urllib.parse import urljoin

from openai import AsyncOpenAI
import chainlit as cl

URL = os.environ.get('WORKSPACE_SERVICE_URL')

client = AsyncOpenAI(base_url=urljoin(URL, "v1"), api_key="YOUR_OPENAI_API_KEY")
cl.instrument_openai()

settings = {
    "temperature": 0.7,
    "max_tokens": 500,
    "top_p": 1,
    "frequency_penalty": 0,
    "presence_penalty": 0,
}

@cl.on_chat_start
async def start_chat():
    models = await client.models.list()
    print(f"Using model: {models}")
    if len(models.data) == 0:
        raise ValueError("No models found")

    global model
    model = models.data[0].id
    print(f"Using model: {model}")

@cl.on_message
async def main(message: cl.Message):
    messages=[
        {
            "content": "You are a helpful assistant.",
            "role": "system"
        },
        {
            "content": message.content,
            "role": "user"
        }
    ]
    msg = cl.Message(content="")

    stream = await client.chat.completions.create(
        messages=messages, model=model,
        stream=True,
        **settings
    )

    async for part in stream:
        if token := part.choices[0].delta.content or "":
            await msg.stream_token(token)
    await msg.update()

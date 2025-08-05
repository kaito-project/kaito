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

import os
from urllib.parse import urljoin

import chainlit as cl
from openai import AsyncOpenAI

URL = os.environ.get("WORKSPACE_SERVICE_URL")

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
    messages = [
        {"content": "You are a helpful assistant.", "role": "system"},
        {"content": message.content, "role": "user"},
    ]
    msg = cl.Message(content="")

    stream = await client.chat.completions.create(
        messages=messages, model=model, stream=True, **settings
    )

    async for part in stream:
        if token := part.choices[0].delta.content or "":
            await msg.stream_token(token)
    await msg.update()

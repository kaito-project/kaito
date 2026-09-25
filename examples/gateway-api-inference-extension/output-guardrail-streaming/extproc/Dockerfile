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

FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir --disable-pip-version-check -r requirements.txt

COPY stream_guard.py ext_proc_stream_server.py ./

ENV KAI_GUARD_PORT=9000
ENV KAI_GUARD_POLICY=/etc/kaito/guardrails/policy.yaml
ENV KAI_GUARD_RUNTIME=/config/stream_guard.json

EXPOSE 9000

CMD ["python", "ext_proc_stream_server.py"]
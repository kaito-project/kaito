# Documentation: Explaining Chat Templates for Transformers

Starting from Transformers version 4.42, the library requires a chat template for chat-based models. This configuration, defined in the Jinja format, specifies constraints for chat message inputs. Understanding and implementing these templates is crucial for optimal performance across various Large Language Models (LLMs) such as OpenAI's ChatGPT, Anthropic's Claude, and Google's Gemini.

## What is a Chat Template?
A chat template is a configuration file that defines how chat messages are structured and passed to the underlying model. This structure ensures that the model receives well-defined inputs for optimal performance. The template is written in the Jinja templating language, allowing for flexible and reusable configurations.

### Key Features of Chat Templates:
1. **Message Role Specification:** Defines whether a message is from the system, user, or assistant.
2. **Input Formatting:** Ensures the input adheres to the expected format by the chat model.
3. **Reusability:** Supports dynamic insertion of values, enabling templates to adapt to different contexts.

---

## Why Are Chat Templates Needed?
Chat templates address the following challenges:
1. **Model-Specific Input Requirements:** Different models may expect different input structures. Templates allow you to standardize the input format.
2. **Improved Usability:** Templates abstract away the complexity of structuring inputs for users.
3. **Dynamic Interaction:** Allows dynamic construction of conversations, including history and roles.

For example, in some models, the input may look like:
```
System: You are a helpful assistant.
User: What's the weather today?
Assistant: The weather is sunny.
```
The template ensures this structure is followed consistently.

---

## Creating a Chat Template
A chat template is defined in a `.jinja` file. Below is an example template:

```jinja
{{ system_message }}
{% for message in messages %}
{{ message.role }}: {{ message.content }}
{% endfor %}
```
### Breakdown:
1. **`{{ system_message }}`**: Placeholder for the system prompt (e.g., "You are a helpful assistant.").
2. **`{% for message in messages %}`**: Iterates over the conversation history.
3. **`{{ message.role }}` and `{{ message.content }}`**: Specifies the role (user or assistant) and the message content.

---

## Using Chat Templates with Inference API

KAITO has already provided chat template options in the inference_api code. Simply use the --chat-template flag to pass the path to your .jinja template file.

Find the existing jinja template in the [`chat_templates`](https://github.com/kaito-project/kaito/tree/main/presets/workspace/inference/chat_templates) directory.

Command Example:

```bash
python3 inference_api.py --model model-name --chat-template /path/to/chat_template.jinja
```

---

## Best Practices
1. **Validate Template Syntax:** Ensure the Jinja syntax is correct to avoid runtime errors.
2. **Include System Prompts:** Always start with a system message to guide the assistant’s behavior.
3. **Use Meaningful Roles:** Clearly define roles (`user`, `assistant`, etc.) for better readability and functionality.

---

## Additional Resources
For more details, refer to the [Transformers Chat Templating Documentation](https://huggingface.co/docs/transformers/v4.43.4/en/chat_templating#templates-for-chat-models).
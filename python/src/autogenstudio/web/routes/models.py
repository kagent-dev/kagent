from typing import Any, Dict, List

from autogen_ext.models.anthropic._model_info import _MODEL_INFO as anthropic_models
from autogen_ext.models.ollama._model_info import _MODEL_INFO as ollama_models
from autogen_ext.models.openai._model_info import _MODEL_INFO as openai_models
from autogen_ext.models.openai._model_info import _MODEL_POINTERS
from fastapi import APIRouter

from kagent.models.vertexai._model_info import _MODEL_INFO as vertexai_models

router = APIRouter()


@router.get("/")
async def list_models() -> Dict[str, List[Dict[str, Any]]]:
    response_ollama = []
    for model_name, model_data in ollama_models.items():
        response_ollama.append(
            {
                "name": model_name,
                "function_calling": model_data["function_calling"],
            }
        )

    final_anthropic_models_map = {}
    for model_name, model_data in anthropic_models.items():
        final_anthropic_models_map[model_name] = {"function_calling": model_data["function_calling"]}

    for short_name, long_name_target in _MODEL_POINTERS.items():
        if short_name.startswith("claude-"):
            if long_name_target in anthropic_models:
                properties = anthropic_models[long_name_target]
                final_anthropic_models_map[short_name] = {"function_calling": properties["function_calling"]}

    response_anthropic = [{"name": name, **props} for name, props in final_anthropic_models_map.items()]

    final_openai_models_map = {}
    for model_name, model_data in openai_models.items():
        final_openai_models_map[model_name] = {"function_calling": model_data["function_calling"]}

    for short_name, long_name_target in _MODEL_POINTERS.items():
        if not short_name.startswith("claude-"):
            if long_name_target in openai_models:
                properties = openai_models[long_name_target]
                final_openai_models_map[short_name] = {"function_calling": properties["function_calling"]}

    response_openai = [{"name": name, **props} for name, props in final_openai_models_map.items()]

    response_vertexai = []
    for model_name, model_data in vertexai_models.items():
        response_vertexai.append(
            {
                "name": model_name,
                "function_calling": model_data["function_calling"],
            }
        )

    return {
        "anthropic": response_anthropic,
        "ollama": response_ollama,
        "openAI": response_openai,
        "azureOpenAI": response_openai,
        "vertexAI": response_vertexai,
    }

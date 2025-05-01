from typing import Any, Dict, List

from autogen_ext.models.anthropic._model_info import _MODEL_INFO as anthropic_models
from autogen_ext.models.ollama._model_info import _MODEL_INFO as ollama_models
from autogen_ext.models.openai._model_info import _MODEL_INFO as openai_models
from fastapi import APIRouter

router = APIRouter()


@router.get("/")
async def list_models() -> Dict[str, List[Dict[str, Any]]]:
    # Get the model names from the model info, also get the  "function_calling" value for each model

    response = {
        "anthropic": [
            {
                "name": model,
                "function_calling": anthropic_models[model]["function_calling"],
            }
            for model in anthropic_models.keys()
        ],
        "ollama": [
            {
                "name": model,
                "function_calling": ollama_models[model]["function_calling"],
            }
            for model in ollama_models.keys()
        ],
        "openAI": [
            {
                "name": model,
                "function_calling": openai_models[model]["function_calling"],
            }
            for model in openai_models.keys()
        ],
        "azureOpenAI": [
            {
                "name": model,
                "function_calling": openai_models[model]["function_calling"],
            }
            for model in openai_models.keys()
        ],
    }

    return response

import logging
import os
import warnings
from pathlib import Path

import pytest
import yaml
from autogen_core import CancellationToken

from kagent.tools.k8s import GenerateResourceTool, GenerateResourceToolConfig, GenerateResourceToolInput, ResourceTypes

from ._test_result import GenerateResourceTestResult
from ._yaml_comparer import YAMLComparer

logger = logging.getLogger(__name__)

TEST_CASES = str(Path(__file__).parent / "testcases")
SIMILARITY_THRESHOLD = 0.6


def load_test_cases(file_path):
    """Load test cases from a YAML file."""
    with open(file_path, "r") as f:
        data = yaml.safe_load(f)

    test_cases = []

    if "test_cases" in data:
        # Get the resource type from metadata
        if "metadata" not in data or "resource_type" not in data["metadata"]:
            raise ValueError(f"Missing 'resource_type' in metadata section for file: {file_path}")

        resource_type = data["metadata"]["resource_type"]

        for test_case in data["test_cases"]:
            # Add the resource type to each test case
            test_case["resource_type"] = resource_type
            test_cases.append(test_case)

        return test_cases

    raise ValueError(f"Unknown test case format in file: {file_path}")


def get_resource_type(resource_string: str):
    for resource_type in ResourceTypes:
        if resource_type.value == resource_string:
            return resource_type
    raise ValueError(f"No matching ResourceType found for {resource_string}")


@pytest.fixture(scope="session")
def tool_config():
    """Test fixture to create a configuration for the GenerateResourceTool."""
    # First try to get the key from environment
    api_key = os.environ.get("OPENAI_API_KEY")

    if not api_key:
        logger.warning("No OpenAI API key found. Tests will be skipped.")
        pytest.skip("No OpenAI API key found")

    return GenerateResourceToolConfig(model="gpt-4o-mini", openai_api_key=api_key, temperature=0.1)


# Load all test cases from the the TEST_CASES folder
test_data = []

for file in Path(TEST_CASES).rglob("*.yaml"):
    logger.info(f"Loading test cases from: {file}")
    test_data.extend([(case, file) for case in load_test_cases(file)])


@pytest.fixture(scope="session", autouse=True)
def report_generation():
    """Generate test report at the end of the test session."""
    yield
    GenerateResourceTestResult.generate_report(SIMILARITY_THRESHOLD)


@pytest.mark.asyncio
@pytest.mark.parametrize("test_case,source_file", test_data)
async def test_generate_resource(test_case, source_file, tool_config) -> None:
    """Test the GenerateResourceTool with various inputs."""
    # Check if we're in the no_fail mode -- this is so we can run all tests and just print warnings
    no_fail = os.environ.get("NO_FAIL", "").lower() in ["1", "true", "yes"]

    test_name = test_case["name"]
    input_description = test_case["input"]
    resource_type_str = test_case.get("resource_type")
    expected_output = test_case["expected_output"]

    tool = GenerateResourceTool(tool_config)
    result = await tool.run(
        args=GenerateResourceToolInput(
            resource_description=input_description, resource_type=get_resource_type(resource_type_str)
        ),
        cancellation_token=CancellationToken(),
    )

    try:
        actual_output = yaml.safe_load(result)
        similarity_score, diff_details = YAMLComparer.compute_similarity(expected_output, actual_output)

        GenerateResourceTestResult.add_result(
            test_name=test_name,
            input_description=input_description,
            resource_type=resource_type_str,
            expected_yaml=expected_output,
            actual_yaml=actual_output,
            similarity_score=similarity_score,
            diff_details=diff_details,
        )

        # Print the summary
        logger.info(f"Similarity Score: {similarity_score:.2%}")

        if not no_fail:
            assert similarity_score >= SIMILARITY_THRESHOLD, (
                f"Similarity score {similarity_score:.2%} is below threshold {SIMILARITY_THRESHOLD:.2%}\n{diff_details}"
            )
        else:
            if similarity_score < SIMILARITY_THRESHOLD:
                logger.warning(
                    f"\033[93mWARNING: Test '{test_name}' has low similarity score: {similarity_score:.2%}\033[0m"
                )

    except Exception as e:
        GenerateResourceTestResult.add_result(
            test_name=test_name,
            input_description=input_description,
            resource_type=resource_type_str,
            expected_yaml=expected_output,
            actual_yaml={"error": str(e), "raw_output": result},
            similarity_score=0.0,
            diff_details=f"Failed to parse output: {str(e)}",
        )

        # but don't fail
        if no_fail:
            logger.warning(f"\033[93mWARNING: Test '{test_name}' encountered an error: {str(e)}\033[0m")
        else:
            pytest.fail(f"Failed to parse result: {str(e)}\nRaw output: {result}")

import difflib
from typing import Any, Dict, Tuple

import yaml


class YAMLComparer:
    """Class to compare YAML structures and compute similarity."""

    @staticmethod
    def _normalize_structure(yaml_dict: Dict[str, Any]) -> Dict[str, Any]:
        """Normalize YAML structure to make comparison more meaningful."""
        if not isinstance(yaml_dict, dict):
            return yaml_dict

        result = {}

        # Process each key-value pair
        for key, value in yaml_dict.items():
            # Skip irrelevant metadata
            if key == "status" or key in ["creationTimestamp", "generation"]:
                continue

            # Process nested dictionaries
            if isinstance(value, dict):
                result[key] = YAMLComparer._normalize_structure(value)
            # Process lists
            elif isinstance(value, list):
                # For simple lists of primitives, sort them
                if all(not isinstance(item, (dict, list)) for item in value):
                    result[key] = sorted(value)
                else:
                    # For lists of dictionaries, normalize each dictionary
                    normalized_list = [
                        YAMLComparer._normalize_structure(item) if isinstance(item, dict) else item for item in value
                    ]
                    result[key] = normalized_list
            else:
                result[key] = value

        return result

    @staticmethod
    def _convert_to_flat_dict(yaml_dict: Dict[str, Any], parent_key: str = "") -> Dict[str, Any]:
        """Convert a nested YAML dict to a flat dictionary with dot-notation keys."""
        items = []
        for key, value in yaml_dict.items():
            new_key = f"{parent_key}.{key}" if parent_key else key

            if isinstance(value, dict):
                items.extend(YAMLComparer._convert_to_flat_dict(value, new_key).items())
            elif isinstance(value, list):
                if all(isinstance(item, dict) for item in value):
                    for i, item in enumerate(value):
                        list_key = f"{new_key}[{i}]"
                        items.extend(YAMLComparer._convert_to_flat_dict(item, list_key).items())
                else:
                    items.append((new_key, value))
            else:
                items.append((new_key, value))

        return dict(items)

    @staticmethod
    def compute_similarity(expected: Dict[str, Any], actual: Dict[str, Any]) -> Tuple[float, str]:
        """Compute similarity between two YAML structures and return a diff."""
        # Normalize structures
        normalized_expected = YAMLComparer._normalize_structure(expected)
        normalized_actual = YAMLComparer._normalize_structure(actual)

        # Convert to flat dictionaries
        flat_expected = YAMLComparer._convert_to_flat_dict(normalized_expected)
        flat_actual = YAMLComparer._convert_to_flat_dict(normalized_actual)

        # Get all unique keys
        all_keys = set(flat_expected.keys()).union(set(flat_actual.keys()))
        total_keys = len(all_keys)

        if total_keys == 0:
            return 1.0, "Both YAMLs are empty"

        # Count matching keys
        matching_keys = 0
        diff_details = []

        for key in sorted(all_keys):
            if key in flat_expected and key in flat_actual:
                expected_value = flat_expected[key]
                actual_value = flat_actual[key]

                if expected_value == actual_value:
                    matching_keys += 1
                else:
                    diff_details.append(f"Value mismatch for key '{key}':")
                    diff_details.append(f"  Expected: {expected_value}")
                    diff_details.append(f"  Actual: {actual_value}")
            elif key in flat_expected:
                diff_details.append(f"Key '{key}' missing in actual YAML")
                diff_details.append(f"  Expected value: {flat_expected[key]}")
            else:  # key in flat_actual
                diff_details.append(f"Key '{key}' unexpected in actual YAML")
                diff_details.append(f"  Actual value: {flat_actual[key]}")

        # Calculate similarity score
        similarity_score = matching_keys / total_keys

        # Generate diff using difflib for a more visual representation
        expected_yaml_str = yaml.dump(normalized_expected, sort_keys=False)
        actual_yaml_str = yaml.dump(normalized_actual, sort_keys=False)

        diff = difflib.unified_diff(
            expected_yaml_str.splitlines(keepends=True),
            actual_yaml_str.splitlines(keepends=True),
            fromfile="expected",
            tofile="actual",
        )

        diff_str = "".join(diff)

        # Combine the text diff with our detailed analysis
        detailed_diff = "\n".join(diff_details)
        full_diff = f"Similarity Score: {similarity_score:.2%}\n\nDetailed Differences:\n{detailed_diff}\n\nUnified Diff:\n{diff_str}"

        return similarity_score, full_diff

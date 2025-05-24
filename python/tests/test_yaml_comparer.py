import pytest
from _yaml_comparer import YAMLComparer

def test_normalize_structure_removes_status_and_metadata():
    input_yaml = {
        "metadata": {"creationTimestamp": "2023-01-01", "name": "foo"},
        "status": {"phase": "Running"},
        "spec": {"replicas": 3}
    }
    expected = {
        "metadata": {"name": "foo"},
        "spec": {"replicas": 3}
    }
    result = YAMLComparer._normalize_structure(input_yaml)
    assert result == expected

def test_normalize_structure_sorts_simple_lists():
    input_yaml = {"list": [3, 1, 2]}
    expected = {"list": [1, 2, 3]}
    result = YAMLComparer._normalize_structure(input_yaml)
    assert result == expected

def test_convert_to_flat_dict():
    input_yaml = {"a": {"b": 1, "c": [2, 3]}, "d": 4}
    expected = {"a.b": 1, "a.c": [2, 3], "d": 4}
    result = YAMLComparer._convert_to_flat_dict(input_yaml)
    assert result == expected

def test_compute_similarity_identical():
    yaml1 = {"a": 1, "b": {"c": 2}}
    yaml2 = {"a": 1, "b": {"c": 2}}
    score, diff = YAMLComparer.compute_similarity(yaml1, yaml2)
    assert score == 1.0
    assert "Similarity Score: 100.00%" in diff

def test_compute_similarity_with_difference():
    yaml1 = {"a": 1, "b": {"c": 2}}
    yaml2 = {"a": 1, "b": {"c": 3}}
    score, diff = YAMLComparer.compute_similarity(yaml1, yaml2)
    assert score < 1.0
    assert "Value mismatch for key 'b.c':" in diff

def test_compute_similarity_missing_key():
    yaml1 = {"a": 1, "b": 2}
    yaml2 = {"a": 1}
    score, diff = YAMLComparer.compute_similarity(yaml1, yaml2)
    assert score < 1.0
    assert "Key 'b' missing in actual YAML" in diff

def test_compute_similarity_unexpected_key():
    yaml1 = {"a": 1}
    yaml2 = {"a": 1, "b": 2}
    score, diff = YAMLComparer.compute_similarity(yaml1, yaml2)
    assert score < 1.0
    assert "Key 'b' unexpected in actual YAML" in diff 
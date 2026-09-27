package after_resolution

import rego.v1

# input.registry holds only the definitions of this registry, not its dependencies.

request_identity := {"enduser.id", "gen_ai.conversation.id", "a2a.task.id"}

unit_suffix := `(_total|\.total|_seconds|_milliseconds|_ms|\.ms|_bytes)$`

deny contains finding if {
	some attr in input.registry.attributes
	not startswith(attr.key, "kagent.")
	not startswith(attr.key, "a2a.")
	finding := {
		"id": "kagent_owned_namespace",
		"context": {"attribute": attr.key},
		"message": sprintf("Attribute '%s' is defined by kagent but is not in the kagent. or a2a. namespace. Reference the upstream attribute instead.", [attr.key]),
		"level": "violation",
	}
}

deny contains finding if {
	some entity in input.registry.entities
	some attr in array.concat(object.get(entity, "identity", []), object.get(entity, "description", []))
	attr.key in request_identity
	finding := {
		"id": "kagent_no_request_identity",
		"context": {"entity": entity.type, "attribute": attr.key},
		"message": sprintf("Entity '%s' carries '%s'. One process serves many requests, so request identity never belongs on a resource.", [entity.type, attr.key]),
		"level": "violation",
	}
}

deny contains finding if {
	some metric in array.concat(input.registry.metrics, input.refinements.metrics)
	some attr in metric.attributes
	attr.key in request_identity
	finding := {
		"id": "kagent_no_request_identity",
		"context": {"metric": metric.name, "attribute": attr.key},
		"message": sprintf("Metric '%s' carries '%s'. Request identity is unbounded and never belongs on a metric.", [metric.name, attr.key]),
		"level": "violation",
	}
}

deny contains finding if {
	some span in array.concat(input.registry.spans, input.refinements.spans)
	not "error.type" in {attr.key | some attr in span.attributes}
	name := object.get(span, "id", span.type)
	finding := {
		"id": "kagent_error_type_on_spans",
		"context": {"span": name},
		"message": sprintf("Span '%s' does not reference error.type. Every kagent span can fail.", [name]),
		"level": "violation",
	}
}

deny contains finding if {
	some metric in array.concat(input.registry.metrics, input.refinements.metrics)
	regex.match(unit_suffix, metric.name)
	finding := {
		"id": "kagent_metric_name_suffix",
		"context": {"metric": metric.name},
		"message": sprintf("Metric '%s' ends in a unit or _total suffix. The unit belongs in the unit field.", [metric.name]),
		"level": "violation",
	}
}

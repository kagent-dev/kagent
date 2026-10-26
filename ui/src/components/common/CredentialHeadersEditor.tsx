import { Button, Form, Input, InputNumber, Select, Space } from "antd";
import type { Header } from "@/api";

/** Edits credential sources without reading or generating credentials in the browser. */
export function CredentialHeadersEditor({ value, onChange, maxHeaders = 16 }: {
  value: Header[];
  onChange: (headers: Header[]) => void;
  maxHeaders?: number;
}) {
  const update = (index: number, header: Header) =>
    onChange(value.map((current, i) => i === index ? header : current));
  return <>
    {value.map((header, index) => <div key={index}>
      <Space align="start" wrap>
        <Form.Item label={`Header ${index + 1} name`}>
          <Input aria-label={`Header ${index + 1} name`} value={header.name} onChange={(event) => update(index, { ...header, name: event.target.value })} />
        </Form.Item>
        <Form.Item label="Prefix">
          <Input aria-label={`Header ${index + 1} prefix`} value={header.prefix ?? ""} onChange={(event) => update(index, { ...header, prefix: event.target.value })} />
        </Form.Item>
        <Form.Item label="Credential source">
          <Select aria-label={`Header ${index + 1} credential source`} value={header.valueFrom.secretKeyRef ? "secret" : header.valueFrom.actorToken ? "actor" : "passthrough"}
            options={[{ value: "secret", label: "Existing Secret" }, { value: "passthrough", label: "Invocation token" }, { value: "actor", label: "Actor token" }]}
            onChange={(source) => update(index, source === "secret"
              ? { name: header.name, prefix: header.prefix, valueFrom: { secretKeyRef: { name: "", key: "" } } }
              : source === "actor"
              ? { name: header.name, prefix: header.prefix, valueFrom: { actorToken: { audiences: [] } } }
              : { name: header.name, prefix: header.prefix, valueFrom: { tokenPassthrough: {} } })} />
        </Form.Item>
        <Button aria-label={`Remove credential header ${index + 1}`} onClick={() => onChange(value.filter((_, i) => i !== index))}>Remove</Button>
      </Space>
      {header.valueFrom.secretKeyRef && <Space wrap>
        <Form.Item label="Secret name">
          <Input aria-label={`Header ${index + 1} Secret name`} value={header.valueFrom.secretKeyRef.name} onChange={(event) => update(index, { name: header.name, prefix: header.prefix, valueFrom: { secretKeyRef: { name: event.target.value, key: header.valueFrom.secretKeyRef?.key ?? "" } } })} />
        </Form.Item>
        <Form.Item label="Secret key">
          <Input aria-label={`Header ${index + 1} Secret key`} value={header.valueFrom.secretKeyRef.key} onChange={(event) => update(index, { name: header.name, prefix: header.prefix, valueFrom: { secretKeyRef: { name: header.valueFrom.secretKeyRef?.name ?? "", key: event.target.value } } })} />
        </Form.Item>
      </Space>}
      {header.valueFrom.actorToken && <Space align="start" wrap>
        <Form.Item label="Audiences" required extra="Enter each audience and press Enter. The token identifies this agent.">
          <Select mode="tags" aria-label={`Header ${index + 1} audiences`} open={false} suffixIcon={null} maxCount={16}
            value={header.valueFrom.actorToken.audiences} placeholder="https://service.example"
            onChange={(audiences: string[]) => update(index, { ...header, valueFrom: { actorToken: { ...header.valueFrom.actorToken, audiences } } })} />
        </Form.Item>
        <Form.Item label="Lifetime (seconds)" extra="Defaults to 900 seconds (15 minutes).">
          <InputNumber aria-label={`Header ${index + 1} lifetime in seconds`} min={300} max={3600} precision={0} placeholder="900"
            value={header.valueFrom.actorToken.expirationSeconds}
            onChange={(expirationSeconds) => update(index, { ...header, valueFrom: { actorToken: { audiences: header.valueFrom.actorToken?.audiences ?? [], expirationSeconds: expirationSeconds ?? undefined } } })} />
        </Form.Item>
      </Space>}
    </div>)}
    <Button disabled={value.length >= maxHeaders} onClick={() => onChange([...value, { name: "", valueFrom: { secretKeyRef: { name: "", key: "" } } }])}>Add credential header</Button>
  </>;
}

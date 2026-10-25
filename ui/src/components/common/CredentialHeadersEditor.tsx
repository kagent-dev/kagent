import { Button, Form, Input, Select, Space } from "antd";
import type { Header } from "@/api";

/** Edits credential references and invocation passthrough without reading secrets. */
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
          <Select aria-label={`Header ${index + 1} credential source`} value={header.valueFrom.secretKeyRef ? "secret" : "passthrough"}
            options={[{ value: "secret", label: "Existing Secret" }, { value: "passthrough", label: "Invocation token" }]}
            onChange={(source) => update(index, source === "secret"
              ? { name: header.name, prefix: header.prefix, valueFrom: { secretKeyRef: { name: "", key: "" } } }
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
    </div>)}
    <Button disabled={value.length >= maxHeaders} onClick={() => onChange([...value, { name: "", valueFrom: { secretKeyRef: { name: "", key: "" } } }])}>Add credential header</Button>
  </>;
}

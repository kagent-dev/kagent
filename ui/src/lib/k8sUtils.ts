import { isResourceNameValid } from "./utils";

export const k8sRefUtils = {
  SEPARATOR: '/',

  toRef(namespace: string, name: string): string {
    if (!namespace || namespace === '') namespace = 'default';

    this.validateInput(namespace, 'namespace');
    this.validateInput(name, 'name');

    return `${namespace}${this.SEPARATOR}${name}`;
  },

  fromRef(ref: string): { namespace: string; name: string } {
    if (!ref || typeof ref !== 'string') {
      throw new Error('Reference must be a non-empty string');
    }

    const separatorIndex = ref.indexOf(this.SEPARATOR);

    if (separatorIndex === -1) {
      throw new Error(`Reference must contain the separator "${this.SEPARATOR}"`);
    }

    if (separatorIndex === 0 || separatorIndex === ref.length - 1) {
      throw new Error('Namespace and name cannot be empty');
    }

    if (ref.indexOf(this.SEPARATOR, separatorIndex + 1) !== -1) {
      throw new Error('Reference can only contain one separator');
    }

    const namespace = ref.substring(0, separatorIndex);
    const name = ref.substring(separatorIndex + 1);

    return { namespace, name };
  },

  isValidRef(ref: string): boolean {
    try {
      this.fromRef(ref);
      return true;
    } catch {
      return false;
    }
  },

  validateInput(value: string, fieldName: string): void {
    if (!value || typeof value !== 'string') {
      throw new Error(`${fieldName} must be a non-empty string`);
    }

    if (value.includes(this.SEPARATOR)) {
      throw new Error(`${fieldName} cannot contain the "${this.SEPARATOR}" character`);
    }

    if (!isResourceNameValid(value)) {
      throw new Error(`${fieldName} must comply with Kubernetes naming rules (RFC 1123 subdomain)`);
    }
  }
};

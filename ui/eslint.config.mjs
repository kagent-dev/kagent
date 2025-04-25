import { dirname } from "path";
import { fileURLToPath } from "url";
import { FlatCompat } from "@eslint/eslintrc";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({
  baseDirectory: __dirname,
});

const eslintConfig = [
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  {
    files: ["**/*.test.ts", "**/*.test.tsx", "**/*.spec.ts", "**/*.spec.tsx"],
    rules: {
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-unused-vars": "off",
      "react-hooks/rules-of-hooks": "off",
      "react-hooks/exhaustive-deps": "off",
      "no-console": "off",
      "jest/no-disabled-tests": "off",
      "jest/no-focused-tests": "off",
      "jest/no-identical-title": "off",
      "jest/prefer-to-have-length": "off",
      "jest/expect-outside": "off",
      "jest/no-conditional-expect": "off",
      "jest/no-done-callback": "off",
      "jest/no-standalone-expect": "off",
      "jest/valid-expect": "off",
      "jest/valid-expect-in-promise": "off",
      "jest/valid-title": "off",
    },
  },
];

export default eslintConfig;

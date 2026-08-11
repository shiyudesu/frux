import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh
    },
    rules: {
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
          varsIgnorePattern: "^_"
        }
      ],
      "react-hooks/exhaustive-deps": "warn",
      "react-hooks/rules-of-hooks": "error",
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }]
    }
  },
  {
    files: [
      "src/admin/VideoOperationsPage.tsx",
      "src/admin/adminSession.tsx",
      "src/components/CollectionQueueViewer.tsx",
      "src/components/FeedActionRail.tsx",
      "src/components/ThreadedComments.tsx",
      "src/feedRefresh.tsx",
      "src/pages/MessagesPage.tsx",
      "src/pages/ProfilePage.tsx",
      "src/router.tsx",
      "src/session.tsx"
    ],
    rules: {
      "react-refresh/only-export-components": "off"
    }
  }
);

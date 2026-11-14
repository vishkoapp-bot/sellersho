import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const entries = [
  'src/components/admin/AdminLoginPage.tsx',
  'src/components/seller/SellerAuthGuard.tsx',
  'src/components/supplier/SupplierAuthGuard.tsx',
  'src/components/warehouse/WarehouseAuthGuard.tsx',
  'src/components/views/SupplierLandingView.tsx',
];

// Security boundary check: a new panel login must not bypass the provider's
// generation/credential guards. Mounted lifecycle tests verify that boundary's
// behavior; this catches alternate entry points silently installing tokens.
for (const path of entries) {
  test(`${path}: credential issuance stays inside shared authentication`, () => {
    const source = ts.createSourceFile(path, readFileSync(path, 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const violations: string[] = [];
    const visit = (node: ts.Node) => {
      if (ts.isImportSpecifier(node) && (node.propertyName?.text || node.name.text) === 'tokenStore') {
        violations.push('panel imports the credential writer');
      }
      if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression) &&
          node.expression.expression.getText(source) === 'authApi' &&
          ['passwordLogin', 'verifyOTP', 'registerSupplier'].includes(node.expression.name.text)) {
        let parent: ts.Node | undefined = node.parent;
        let guarded = false;
        while (parent) {
          if (ts.isCallExpression(parent) && parent.expression.getText(source) === 'authenticate') {
            guarded = true;
            break;
          }
          parent = parent.parent;
        }
        if (!guarded) violations.push(`unguarded ${node.expression.name.text}`);
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
    assert.deepEqual(violations, []);
  });
}

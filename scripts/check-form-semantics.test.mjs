import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { checkFormSemanticsSource } from "./check-form-semantics.mjs";

function failures(source) {
  return checkFormSemanticsSource(source, "fixture.tsx");
}

describe("form semantics checker", () => {
  it("accepts a validated form submission", () => {
    assert.deepEqual(
      failures(`
        const submit = handleSubmit(async (value) => save.mutateAsync(value));
        const view = <form onSubmit={submit}>
          <Field requirement="required" label="Name"><Input /></Field>
          <Button type="submit">Save</Button>
        </form>;
      `),
      [],
    );
  });

  it("rejects editable forms without a submit boundary", () => {
    assert.match(
      failures(`
        const view = <FormDrawer open>
          <Field requirement="required" label="Name"><Input /></Field>
        </FormDrawer>;
      `).join("\n"),
      /must declare an onSubmit handler/,
    );
  });

  it("rejects direct mutation buttons next to editable fields", () => {
    assert.match(
      failures(`
        const view = <div className="editor">
          <Field requirement="required" label="Name"><Input /></Field>
          <Button onClick={() => save.mutate({})}>Save</Button>
        </div>;
      `).join("\n"),
      /must submit through a validated form/,
    );
  });

  it("rejects indirect unvalidated API write handlers", () => {
    assert.match(
      failures(`
        const save = async () => api.org.createRole({});
        const view = <div className="editor">
          <Field requirement="required" label="Name"><Input /></Field>
          <Button onClick={() => void save()}>Save</Button>
        </div>;
      `).join("\n"),
      /must submit through a validated form/,
    );
  });

  it("allows command buttons and alternate conditional branches", () => {
    assert.deepEqual(
      failures(`
        const begin = async () => api.auth.enrollTotp();
        const view = <div>{enabled ? (
          <form onSubmit={handleSubmit(save)}>
            <Field requirement="required" label="Code"><Input /></Field>
            <Button type="submit">Verify</Button>
          </form>
        ) : (
          <Button onClick={() => void begin()}>Enroll</Button>
        )}</div>;
        const command = <ConfirmDialog onConfirm={() => remove.mutate(id)} />;
      `),
      [],
    );
  });
});

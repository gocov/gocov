import { Card, FormField, SaveFooter, Checkbox, TextInput } from "gocov-web";

export function InASettingsCard() {
  return (
    <Card>
      <Card.Header title="General" />
      <Card.Body>
        <FormField label="Base branch" help="The branch every other branch is compared against.">
          {(field) => <TextInput {...field} defaultValue="main" />}
        </FormField>
      </Card.Body>
      <Card.Footer>
        <SaveFooter
          owner
          hint="Applies to the next upload."
          ownerOnly="Owners set the base branch."
          busy={false}
          saving={false}
          saved={false}
          onSave={() => {}}
        />
      </Card.Footer>
    </Card>
  );
}

export function SavingAndWaiting() {
  return (
    <div className="stack">
      <Card>
        <Card.Header title="Coverage gates" />
        <Card.Footer>
          <SaveFooter
            owner
            hint="Applies to uploads received from now on. Past verdicts are not recalculated."
            ownerOnly="Owners set the gates."
            busy
            saving
            saved={false}
            onSave={() => {}}
          />
        </Card.Footer>
      </Card>
      <Card>
        <Card.Header title="Ignored files" />
        <Card.Footer>
          <SaveFooter
            owner
            hint="Waits until the gates finish saving."
            ownerOnly="Owners set the ignore patterns."
            busy
            saving={false}
            saved={false}
            onSave={() => {}}
          />
        </Card.Footer>
      </Card>
    </div>
  );
}

export function Saved() {
  return (
    <Card>
      <Card.Header title="Ignored files" />
      <Card.Footer>
        <SaveFooter
          owner
          hint="Applies to uploads received from now on. Past reports keep their numbers."
          ownerOnly="Owners set the ignore patterns."
          busy={false}
          saving={false}
          saved
          onSave={() => {}}
        />
      </Card.Footer>
    </Card>
  );
}

export function MemberReadOnly() {
  return (
    <Card>
      <Card.Header title="Public reports" />
      <Card.Body>
        <Checkbox
          label="Serve read-only report pages to visitors who are not signed in"
          checked={false}
          disabled
          onChange={() => {}}
        />
      </Card.Body>
      <Card.Footer>
        <SaveFooter
          owner={false}
          hint="Turning this off closes the pages to members only immediately."
          ownerOnly="Owners decide."
          busy={false}
          saving={false}
          saved={false}
          onSave={() => {}}
        />
      </Card.Footer>
    </Card>
  );
}

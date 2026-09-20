import { FormField, Textarea, TextInput } from "gocov-web";

export function LabelAndHelp() {
  return (
    <FormField label="Default branch" help="Trends and gate baselines are measured against this branch.">
      {(field) => <TextInput {...field} defaultValue="main" />}
    </FormField>
  );
}

export function WithError() {
  return (
    <FormField label="Ignore paths" help="One pattern per line." error="“vendor/**/” is not a valid pattern.">
      {(field) => <Textarea {...field} mono defaultValue={"vendor/**/"} />}
    </FormField>
  );
}

export function LabelOnly() {
  return (
    <FormField label="Comment heading">
      {(field) => <TextInput {...field} defaultValue="Coverage report" />}
    </FormField>
  );
}

// A settings page is one document edited through several cards, each with
// its own Save button: the form is shared, and whichever button is pressed
// posts all of it. This is that machine — the form, which button is
// working, and which one last succeeded — for both settings pages.

import { useMutation } from "@tanstack/react-query";
import { useState } from "react";

interface Options<Input, Doc> {
  /** The form as the loaded document has it. */
  seed: () => Input;
  post: (input: Input) => Promise<Doc>;
  /** The saved document came back: store it, and return the form it seeds. */
  onSaved: (doc: Doc) => Input;
}

/** What one card's Save button needs to know; spread it into SaveFooter. */
export interface SectionSaveState {
  /** Some section is saving, so every Save waits. */
  busy: boolean;
  /** This section's button is the one that was pressed. */
  saving: boolean;
  /** This section's save was the last to succeed, and nothing changed since. */
  saved: boolean;
  onSave: () => void;
}

export function useSectionSave<Input extends object, Doc>({ seed, post, onSaved }: Options<Input, Doc>) {
  const [form, setForm] = useState<Input>(seed);
  const [pressed, setPressed] = useState("");
  const [savedIn, setSavedIn] = useState("");

  const save = useMutation({
    mutationFn: post,
    onSuccess: (doc) => {
      setForm(onSaved(doc));
      setSavedIn(pressed);
    },
  });

  function update(patch: Partial<Input>) {
    setSavedIn("");
    setForm((f) => ({ ...f, ...patch }));
  }

  const section = (id: string): SectionSaveState => ({
    busy: save.isPending,
    saving: save.isPending && pressed === id,
    saved: savedIn === id,
    onSave: () => {
      setPressed(id);
      setSavedIn("");
      save.mutate(form);
    },
  });

  return { form, update, section, error: save.isError ? save.error : null };
}

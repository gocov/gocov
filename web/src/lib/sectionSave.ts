// A settings page is one document edited through several cards, each with
// its own Save button: the form is shared, and whichever button is pressed
// posts all of it. This is that machine — the form, which button is
// working, and which one last succeeded — for both settings pages.

import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router";
import { apiPost } from "@/lib/api/client";
import { postToken } from "@/lib/api/queries";
import { routes } from "@/lib/urls";

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

interface SettingsDocOptions<Input, Doc> {
  /** The settings document as it loaded, and the query it is cached under. */
  doc: Doc;
  queryKey: QueryKey;
  /** The document's endpoint for an action: "save", "delete", "reveal-token", "rotate-token". */
  path: (action: string) => string;
  /** The form a document seeds. */
  toInput: (doc: Doc) => Input;
}

/**
 * Everything a settings page does with its document besides drawing it:
 * the shared form behind its Save buttons (useSectionSave), which caches
 * the saved document; removing it, which lands on the dashboard; and
 * revealing or rotating its token — a rotation outdates the cached masked
 * form, while the token itself never enters the cache.
 */
export function useSettingsDoc<Input extends object, Doc>({ doc, queryKey, path, toInput }: SettingsDocOptions<Input, Doc>) {
  const client = useQueryClient();
  const navigate = useNavigate();

  const sections = useSectionSave({
    seed: () => toInput(doc),
    post: (input: Input) => apiPost<Doc>(path("save"), input),
    onSaved: (next) => {
      client.setQueryData(queryKey, next);
      return toInput(next);
    },
  });

  const remove = useMutation({
    mutationFn: () => apiPost<void>(path("delete")),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["dashboard"] });
      void navigate(routes.dashboard());
    },
  });

  return {
    ...sections,
    remove: () => remove.mutateAsync(),
    revealToken: () => postToken(path("reveal-token")),
    rotateToken: () =>
      postToken(path("rotate-token")).then((token) => {
        void client.invalidateQueries({ queryKey });
        return token;
      }),
  };
}

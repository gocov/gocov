// One stroke icon set on a 16px grid, coloured by currentColor. No emoji,
// no glyph characters: every symbol the UI shows comes from here.
const paths = {
  check: "M3.5 8.5l3 3 6-7",
  cross: "M4 4l8 8M12 4l-8 8",
  "caret-down": "M4 6l4 4 4-4",
  "caret-right": "M6 4l4 4-4 4",
  "arrow-up": "M8 13V3M4 7l4-4 4 4",
  "arrow-down": "M8 3v10M4 9l4 4 4-4",
  "arrow-left": "M13 8H3M7 4L3 8l4 4",
  "arrow-right": "M3 8h10M9 4l4 4-4 4",
  search: "M11.5 7a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0zM10.5 10.5L14 14",
  external: "M6.5 3.5h-3v9h9v-3M9.5 3.5h3v3M12.5 3.5L7.5 8.5",
  copy: "M5.5 5.5h8v8h-8zM10.5 5.5v-2a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2",
  eye: "M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8s-2.4 4.5-6.5 4.5S1.5 8 1.5 8zM10 8a2 2 0 1 1-4 0 2 2 0 0 1 4 0z",
  folder: "M1.5 4.5a1 1 0 0 1 1-1h3l1.5 1.5h6.5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-11a1 1 0 0 1-1-1z",
  file: "M4 1.5h5l3.5 3.5v8.5a1 1 0 0 1-1 1h-7.5a1 1 0 0 1-1-1v-11a1 1 0 0 1 1-1zM9 1.5V5h3.5",
  warning: "M8 3.5v5.5M8 11.8v.4",
  info: "M8 7.2v4.6M8 4.2v.4",
  minus: "M3.5 8h9",
  "eye-off": "M2 2l12 12M6.6 6.6a2 2 0 0 0 2.8 2.8M4.3 4.4C2.7 5.5 1.5 8 1.5 8s2.4 4.5 6.5 4.5c1.2 0 2.3-.4 3.2-.9M13.2 10.3c.9-1 1.3-2.3 1.3-2.3s-2.4-4.5-6.5-4.5c-.5 0-1 .07-1.4.19",
  person: "M8 8.5a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2zM2.8 13.5c.7-2.3 2.7-3.5 5.2-3.5s4.5 1.2 5.2 3.5",
  bot: "M5 5.5h6a1.5 1.5 0 0 1 1.5 1.5v4A1.5 1.5 0 0 1 11 12.5H5A1.5 1.5 0 0 1 3.5 11V7A1.5 1.5 0 0 1 5 5.5zM8 3.2v2.3M6.2 8.6v.5M9.8 8.6v.5",
} as const;

export type IconName = keyof typeof paths;

export function Icon({ name, size = 14, label }: { name: IconName; size?: number; label?: string }) {
  return (
    <svg
      className="Icon"
      viewBox="0 0 16 16"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <path d={paths[name]} />
    </svg>
  );
}

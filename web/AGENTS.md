# web/AGENTS.md: page rules

Rules for every change to the files under this directory: the page the app shows in its WebView2
window. It is plain TypeScript without a framework (`src/main.ts`), styled by `src/style.css`,
translated by `src/i18n.ts`, and talks to Go through the generated `src/mygo.ts`. The root
`AGENTS.md` (rule priority, workflow, writing, commits) applies as well; numbers are in
`REQUIREMENTS.md`.

## Page

### Components and modularity

- Reuse the existing controls and helpers in `main.ts` and the classes in `style.css`; do not
  build a second version of one.
- Finish any existing modularization; no half-done work, such as colors or strings only partly
  extracted with the rest hardcoded.
- No literal text outside `i18n.ts`, and no literal color outside the custom properties on
  `:root` in `style.css`.
- A new page follows the layout of the existing pages in the side rail.

### Consistency

- Animations (if present) are unified in duration, easing and triggers. Animate only transform and
  opacity, never make the user wait for an exit, and respect reduced motion.
- One visual style, in both the normal and the glass (`:root.glass`) appearance.
- Notices are unified and used for their real meaning: info, warning, error, success.

### Layout, type and loading

The values (sizes, gaps, window sizes) live in `style.css` or `REQUIREMENTS.md`. These are the
rules those values must satisfy.

- **Type scale.**
  - Every text size and line height comes from one named scale: page title, section heading, card
    title, body, small or meta.
  - A role has one size on every page.
  - New code takes a size already used for its role; it adds no new literal size.
- **Spacing.**
  - Every gap and padding comes from one spacing scale.
  - The same relationship gets the same gap everywhere: between page sections, between cards,
    inside a card, between a label and its control.
  - New code takes a gap already used for the same relationship.
- **Page titles.**
  - Every page has exactly one title, in the same position, size and distance from the content.
  - The page's actions sit at the end of the title row.
- **One width per kind of page.**
  - All pages of one kind share one content width, so the title row and the body end on the same
    edge.
  - A form that reads badly when stretched sits in a readable column.
  - Lists and key maps span the content area.
- **No large blank areas.**
  - At every supported window size (the minimum, the default, maximized on a wide screen), the
    content fills its area by its natural layout. Otherwise the area is narrower.
  - A half-empty window is a defect, and so is a side gap that only one page has.
- **Alignment.**
  - Everything on a page lines up on a few shared edges.
  - A column's content aligns with its header: text and actions from the start, numbers at the
    end.
  - Labels and controls of one row share a baseline.
- **Centring.**
  - Content meant to be centred (an empty state, a dialog, the device picker) is centred both
    horizontally and vertically, by flex or grid, never by guessed padding.
  - Icon and text inside a control are vertically centred. Check this with Chinese and Japanese
    text, whose metrics differ from Latin.
- **Values never break.**
  - DPI values, polling rates, key names, versions and dates never wrap in the middle.
  - Truncated text shows its full value on hover and on focus.
- **Loading.** Page-level loading uses the page's skeleton; an action on a control (a button, a
  key) shows its loading state on that control. Neither uses a full-window spinner.
  - A page's skeleton has its final frame, title and sections at their final sizes, shown from
    the first paint.
  - Nothing moves when the device answers: no layout shift.
  - "No device connected" and a failed read are designed states of their own, never a skeleton
    that stays.
- **Feedback.**
  - Every click visibly responds.
  - A write to a device shows its progress, then its success or failure; no silent buttons.
  - One primary action per view.
  - Labels name the action; never "OK".
  - Irreversible actions (factory reset, deleting a scene or macro, importing a backup over the
    current settings) ask for confirmation.
- **Minimum window size.**
  - At the minimum size in `REQUIREMENTS.md`: no horizontal scroll and nothing cut off.
  - No information is reachable only by hover.
- **Checked by screenshot.**
  - Before review, shoot each changed page at the minimum and the default window size, in the
    normal and the glass appearance, in English and in one CJK locale, with a real device.
  - A shot of "no device connected" does not count as checking a device page.
  - Show a new page or flow to the owner as one screen first, before building the rest.

### i18n (if present)

- No missing translations and no hardcoded strings. Every locale in `i18n.ts` (zh-CN, en, ja) has
  every key; the typecheck enforces this through `Record<Key, string>`.
- Consistent key naming; no duplicate or unused keys.
- Device and key names from the catalogs stay untranslated.

### Usability

- Generic and easy to use.
- Before designing a flow, compare how mainstream apps do it (Windows Settings, Razer Synapse,
  Logitech G HUB, VIA). Link them, follow them, and justify any divergence.
- Works at every supported window size and in both appearances.

### Accessibility

- Every action works with the keyboard alone, with visible focus.
- After switching pages, focus starts at the page title.
- WCAG AA contrast in both appearances: 4.5:1 for normal text, 3:1 for large text, 3:1 for UI
  components and focus indicators against their surroundings. In the glass appearance, measure
  against a light and a dark desktop behind the window.

### Performance

- Watch bundle size, render cost and memory against the baseline. The window's memory is freed
  when it closes; nothing the page starts may keep it alive.
- No unnecessary dependencies; the page has one runtime dependency (`mygo-runtime`).

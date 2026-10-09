| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(ui): budget the extension points journey now that its file holds a second test` | upstream added a second test to `extension-points.withExtension.spec.ts`, so its eleven-step journey no longer carried the file's lifecycle budget and the layout convention of `test(ui): count a journey's steps per test` failed on every run; the journey sets the budget in its body | fork-only: the convention is this line's |

> [!IMPORTANT]
> **This PR must target the `dev` branch.** `main` only moves on releases. If the base branch above says `main`, change it to `dev` (the base dropdown next to the PR title).

## Summary

<!-- What does this PR do? Link the issue it addresses if applicable. -->

Closes chr0nzz/traefik-manager#

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Docs update
- [ ] Refactor / cleanup

## Testing

<!-- How did you test this? Which install modes did you try? -->

- [ ] `go test ./...` passes
- [ ] Tried the change on a real install, in a VM or on a throwaway host
- [ ] Regenerated `testdata/golden` if the generated files changed, and checked the diff

## Checklist

- [ ] PR targets the `dev` branch (never `main`)
- [ ] Opened a matching PR for `docs/tm-cli.md` in [traefik-manager](https://github.com/chr0nzz/traefik-manager) if behaviour changed
- [ ] No code comments, commented-out code or debug statements left in

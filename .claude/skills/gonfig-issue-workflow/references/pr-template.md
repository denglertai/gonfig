# Pull request description template

Adapt the sections to the change; drop the ones that don't apply. Keep claims verifiable: every number and example comes from a real run.

~~~~markdown
Closes #<issue>

## ⚠️ Breaking change            <!-- only if behavior changes incompatibly; lead with it -->

**<What changed, in one bold sentence.>** <Why, and what users have to do.>

```console
$ <command showing the new behavior>
<real output>
```

## Summary

<One or two paragraphs: what the change does and why.>

```yaml
# <short example input>
```

```console
$ <command>
<real output>
```

## Changes                       <!-- or "Behavior" for features -->

- **<Area>:** <what changed and why>

## Behavior change               <!-- non-breaking but noticeable changes -->

**<Change>.** <Before vs. after.>

## Performance                   <!-- when hot paths or scaling are affected -->

| Benchmark | Before | After |
| --- | ---: | ---: |

## Verification

- **Tests:** <which tests were added, what they cover>
- **Fuzzing / snapshots / docs / benchmarks:** <what was run, with numbers>
- `go test ./...` (also shuffled) and `go vet` pass.

## Related

- #<issue>: <how it relates>

🤖 Generated with [Claude Code](https://claude.com/claude-code)
~~~~

Use the attribution line the environment specifies; the one above is the current default.

# TODO

- **Converge declared services when a revert releases a hold.** ADR-0066 removes
  services a hand-applied held version added, but a declared service that version
  changed (or, with `rollback: false`, the failed version left running) keeps
  running it after the revert, because the unchanged path makes no `up`. Decide
  whether a hold release should run a real converge (`up -d --remove-orphans`
  through the apply path, stopping `on_demand_containers` again), and record it
  in an ADR.

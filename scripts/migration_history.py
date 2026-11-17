"""Fail-closed classification for canonical and reviewed legacy history."""


def classify_history(applied, canonical, aliases):
    applied = set(applied)
    canonical = set(canonical)
    accepted_aliases = set()

    unknown = applied - canonical
    for name in unknown:
        target = aliases.get(name)
        if target is None or target not in canonical:
            raise ValueError(f"unreviewed migration history: {name}")
        if target not in applied:
            raise ValueError(
                f"legacy alias {name} canonical target is not applied: {target}"
            )
        accepted_aliases.add(name)

    return sorted(canonical - applied), accepted_aliases

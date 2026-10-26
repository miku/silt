"""Average new packages per month, by year, from registry snapshots.

Reads data/growth.tsv (registry, date, count), writes
data/growth-per-month.tsv for plots/growth.gp (one row per year, one column
per registry) and prints the numbers the README quotes.

A year runs from the snapshot nearest its 1 January to the one nearest the
next 1 January; the current year runs to the latest snapshot. The growth in
between is scaled to an average month, so windows of different lengths
compare.
"""

import csv
import datetime

REGISTRIES = ["pypi", "crates.io", "packagist"]
YEARS = range(2022, 2027)
MONTH = 365.25 / 12
MAX_OFFSET = 75  # days a snapshot may lie away from 1 January


def load(path):
    snaps = {}
    with open(path) as f:
        for row in csv.reader(f, delimiter="\t"):
            if not row or row[0].startswith("#"):
                continue
            date = datetime.date.fromisoformat(row[1])
            snaps.setdefault(row[0], []).append((date, int(row[2])))
    for s in snaps.values():
        s.sort()
    return snaps


def nearest(snaps, day):
    snap = min(snaps, key=lambda s: abs((s[0] - day).days))
    return snap if abs((snap[0] - day).days) <= MAX_OFFSET else None


def per_month(start, end):
    return (end[1] - start[1]) / (end[0] - start[0]).days * MONTH


def year_window(snaps, year):
    start = nearest(snaps, datetime.date(year, 1, 1))
    if snaps[-1][0].year == year:
        end = snaps[-1]
    else:
        end = nearest(snaps, datetime.date(year + 1, 1, 1))
    if start is None or end is None or end[0] <= start[0]:
        return None
    return start, end


def main():
    snaps = load("data/growth.tsv")
    latest = max(s[-1][0] for s in snaps.values())

    rates = {}
    with open("data/growth-per-month.tsv", "w") as f:
        f.write(f"# latest {latest:%-d %b %Y}\n")
        f.write("# year\t" + "\t".join(REGISTRIES) + "\n")
        for year in YEARS:
            cells = [str(year)]
            for reg in REGISTRIES:
                window = year_window(snaps[reg], year)
                rate = per_month(*window) if window else None
                rates[reg, year] = rate
                cells.append("NaN" if rate is None else f"{rate:.0f}")
            f.write("\t".join(cells) + "\n")

    for reg in REGISTRIES:
        line = ", ".join(
            f"{year}: {rates[reg, year]:,.0f}" for year in YEARS if rates[reg, year]
        )
        print(f"{reg} per month: {line}")
        if rates[reg, 2024] and rates[reg, 2026]:
            print(f"  2026 vs 2024: {rates[reg, 2026] / rates[reg, 2024]:.1f}x")

    crates = snaps["crates.io"]
    june = nearest(crates, datetime.date(2026, 6, 1))
    if june and rates["crates.io", 2024]:
        since_june = per_month(june, crates[-1])
        print(
            f"crates.io since {june[0]}: {since_june:,.0f} per month, "
            f"{since_june / rates['crates.io', 2024]:.1f}x the 2024 rate"
        )
    for reg in REGISTRIES:
        start = nearest(snaps[reg], datetime.date(latest.year, 1, 1))
        last = snaps[reg][-1]
        if start:
            print(
                f"{reg}: {last[1]:,} on {last[0]}, "
                f"+{last[1] - start[1]:,} since {start[0]}"
            )


if __name__ == "__main__":
    main()

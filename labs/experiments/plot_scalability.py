"""Plot logged lookup costs against an illustrative logarithmic reference."""
import argparse
import csv
import math
from collections import defaultdict
from pathlib import Path
import statistics
import sys

deps = Path(__file__).resolve().parents[1] / '.plot-deps'
if deps.exists():
    sys.path.insert(0, str(deps))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, default=Path('experiments/scalability-1000'))
    root = parser.parse_args().input
    groups = defaultdict(list)
    with (root / 'lookup_raw.csv').open(newline='', encoding='utf-8') as handle:
        for row in csv.DictReader(handle):
            if row['event'] == 'result':
                groups[(int(row['node_count']), row['lookup_type'])].append(row)
    if not groups:
        raise ValueError('No logged lookup results found')
    configs = {(row['alpha'], row['k']) for rows in groups.values() for row in rows}
    if len(configs) != 1:
        raise ValueError('Use one alpha/k configuration for this comparison')
    alpha, k = next(iter(configs))
    with (root / 'comparison.csv').open('w', newline='', encoding='utf-8') as handle:
        writer = csv.writer(handle)
        writer.writerow(['node_count', 'lookup_type', 'samples', 'success_rate',
                         'mean_probes', 'mean_estimated_rounds', 'sd_estimated_rounds',
                         'expected_log2_reference'])
        for (size, kind), rows in sorted(groups.items()):
            rounds = [int(row['hop']) for row in rows]
            writer.writerow([size, kind, len(rows),
                             sum(row['success'] == 'true' for row in rows) / len(rows),
                             statistics.mean(int(row['metric_count']) for row in rows),
                             statistics.mean(rounds), statistics.stdev(rounds) if len(rounds) > 1 else 0,
                             math.log2(size)])
    plt.rcParams.update({'font.size': 10, 'svg.fonttype': 'none'})
    fig, axes = plt.subplots(1, 2, figsize=(12, 5), layout='constrained')
    for ax, kind in zip(axes, ('node', 'value')):
        sizes = sorted(n for n, lookup_type in groups if lookup_type == kind)
        means = [statistics.mean(int(r['hop']) for r in groups[(n, kind)]) for n in sizes]
        ax.plot(sizes, means, 'o-', linewidth=2, label='Measured from RPC logs')
        ax.plot(sizes, [math.log2(n) for n in sizes], '--', linewidth=2,
                label='Expected growth reference: log2(N)')
        ax.set(title=f'{kind.capitalize()} lookup', xlabel='Network size (nodes)',
               ylabel='Mean estimated rounds: ceil(probes / alpha)')
        ax.set_xscale('log', base=10)
        ax.set_xticks(sizes, [str(n) for n in sizes])
        ax.set_ylim(bottom=0)
        ax.grid(alpha=.25)
        ax.legend(fontsize=8)
    fig.suptitle(f'Lookup scalability up to 1,000 simulated nodes (alpha={alpha}, k={k})')
    for suffix in ('png', 'svg'):
        fig.savefig(root / f'scalability.{suffix}', dpi=180)
    plt.close(fig)
    print(f'Wrote {root / "comparison.csv"} and scalability.png/.svg')


if __name__ == '__main__':
    main()

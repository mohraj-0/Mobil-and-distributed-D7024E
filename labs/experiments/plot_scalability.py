"""Plot saved lookup results: python experiments/plot_scalability.py --input experiments/scalability-1000."""
import argparse
import csv
import math
from pathlib import Path
import statistics
import sys
from collections import defaultdict

dependencies = Path(__file__).resolve().parents[1] / '.plot-deps'
if dependencies.exists():
    sys.path.insert(0, str(dependencies))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, default=Path(__file__).parent / 'scalability-1000')
    root = parser.parse_args().input
    groups = defaultdict(list)
    seeds = defaultdict(set)
    with (root / 'lookup_raw.csv').open(newline='', encoding='utf-8') as handle:
        for row in csv.DictReader(handle):
            if row['event'] == 'result':
                key = (int(row['node_count']), int(row['alpha']), int(row['k']), row['lookup_type'])
                groups[key].append(math.ceil(int(row['metric_count']) / key[1]))
                seeds[key].add(row['seed'])
    if not groups:
        raise ValueError('No lookup result events found')
    plt.rcParams.update({'svg.fonttype': 'none', 'font.size': 10})
    fig, ax = plt.subplots(figsize=(9, 5.5), layout='constrained')
    for alpha, k, kind in sorted({key[1:] for key in groups}):
        keys = sorted(key for key in groups if key[1:] == (alpha, k, kind))
        ax.errorbar([key[0] for key in keys],
                    [statistics.mean(groups[key]) for key in keys],
                    yerr=[statistics.stdev(groups[key]) if len(groups[key]) > 1 else 0 for key in keys],
                    marker='o', capsize=4,
                    label=f'{kind.capitalize()} lookup (alpha={alpha}, k={k})')
    sizes = sorted({key[0] for key in groups})
    ax.plot(sizes, [math.log2(n) for n in sizes], '--', color='grey',
            label='log2(N) growth reference')
    ax.set_xscale('log', base=2)
    ax.set_xticks(sizes, [str(n) for n in sizes])
    ax.set(xlabel='Network size N (logarithmic scale)', ylabel='Mean estimated lookup rounds',
           title='Lookup scalability')
    ax.set_ylim(bottom=0)
    ax.grid(alpha=.25)
    ax.legend()
    counts = sorted({len(values) for values in groups.values()})
    seed_counts = sorted({len(values) for values in seeds.values()})
    fig.supxlabel(f'Bars: ±1 sample SD across individual lookups; samples per point: {counts}; seeds: {seed_counts}.\n'
                  'Rounds estimated as ceil(RPC probes / alpha); log2(N) is a growth reference.', fontsize=9)
    for extension in ('png', 'svg'):
        output = root / f'lookup_scalability.{extension}'
        fig.savefig(output, dpi=180)
        print(output.resolve())
    plt.close(fig)


if __name__ == '__main__':
    main()

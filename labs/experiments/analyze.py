"""Analyze the mandatory experiments from their CSV event logs.

Run from labs: python experiments/analyze.py --input experiments/results
Dependencies: python -m pip install -r experiments/requirements.txt
"""
import argparse
import csv
from collections import defaultdict
from pathlib import Path
import math
import statistics
import sys

# Support the isolated dependency directory used for local verification.
local_dependencies = Path(__file__).resolve().parents[1] / '.plot-deps'
if local_dependencies.exists():
    sys.path.insert(0, str(local_dependencies))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt


def summarize(groups):
    result = {}
    for key, per_seed in groups.items():
        samples = [value for values in per_seed.values() for value in values]
        seed_means = [statistics.mean(values) for values in per_seed.values()]
        result[key] = {
            'samples': len(samples), 'seeds': len(seed_means),
            'mean': statistics.mean(samples),
            'variance': statistics.variance(samples) if len(samples) > 1 else 0,
            'seed_variance': statistics.variance(seed_means) if len(seed_means) > 1 else 0,
        }
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, default=Path('experiments/results'))
    args = parser.parse_args()
    root = args.input
    scalability = defaultdict(lambda: defaultdict(list))
    reliability = defaultdict(lambda: defaultdict(list))
    with (root / 'lookup_raw.csv').open(newline='', encoding='utf-8') as handle:
        for row in csv.DictReader(handle):
            if row['event'] == 'result':
                key = (int(row['node_count']), int(row['alpha']), int(row['k']), row['lookup_type'])
                scalability[key][row['seed']].append(int(row['metric_count']))
    with (root / 'resilience' / 'trials.csv').open(newline='', encoding='utf-8') as handle:
        for row in csv.DictReader(handle):
            if row['axis'] == 'latency_loss':
                key = (int(row['nodes']), int(row['alpha']), int(row['k']), float(row['loss']), float(row['latency_ms']), row['type'])
                reliability[key][row['seed']].append(float(row['success'] == 'true'))
    scale, loss = summarize(scalability), summarize(reliability)
    if not scale or not loss:
        raise ValueError('Both mandatory experiments must have recorded samples')

    plt.rcParams.update({'font.size': 10, 'svg.fonttype': 'none'})
    fig, ax = plt.subplots(figsize=(8, 5), layout='constrained')
    configurations = sorted({(a, k, kind) for n, a, k, kind in scale})
    for alpha, k, kind in configurations:
        keys = sorted(key for key in scale if key[1:] == (alpha, k, kind))
        ax.errorbar([key[0] for key in keys], [scale[key]['mean'] for key in keys],
                    yerr=[math.sqrt(scale[key]['seed_variance']) for key in keys],
                    marker='o', capsize=3, label=f'{kind}, alpha={alpha}, k={k}')
    sizes = sorted({key[0] for key in scale})
    ax.plot(sizes, [math.log2(n) for n in sizes], '--', color='grey', label='log2(N) reference')
    ax.set(xlabel='Network size N', ylabel='Mean lookup probes', title='Lookup scalability (bars: SD across seed means)')
    ax.set_xscale('log', base=2)
    ax.set_xticks(sizes, [str(n) for n in sizes])
    ax.grid(alpha=.2)
    ax.legend()
    fig.savefig(root / 'scalability.svg')
    fig.savefig(root / 'scalability.png', dpi=180)
    plt.close(fig)

    fig, ax = plt.subplots(figsize=(8, 5), layout='constrained')
    for n, alpha, k, latency, kind in sorted({(key[0], key[1], key[2], key[4], key[5]) for key in loss}):
        keys = sorted((key for key in loss if key[:3] == (n, alpha, k) and key[4:] == (latency, kind)), key=lambda key: key[3])
        ax.errorbar([key[3] for key in keys], [loss[key]['mean'] for key in keys],
                    yerr=[math.sqrt(loss[key]['seed_variance']) for key in keys], marker='o', capsize=3,
                    label=f'{kind}, N={n}, alpha={alpha}, k={k}, delay={latency:g}ms')
    ax.set(xlabel='Independent packet loss probability', ylabel='Lookup success rate',
           title='Lookup reliability (bars: SD across seed rates)', ylim=(-.05, 1.05))
    ax.grid(alpha=.2)
    ax.legend(fontsize=8)
    fig.savefig(root / 'reliability.svg')
    fig.savefig(root / 'reliability.png', dpi=180)
    plt.close(fig)

    lines = ['# Analysis of mandatory experiments', '',
             'Computed by the external Python script from result events and trial logs. ',
             'Sample variance uses n-1. Error bars show the standard deviation of the per-seed means/rates, not a confidence interval.', '',
             '![Scalability](scalability.svg)', '',
             '| N | alpha | k | type | samples | seeds | mean probes | probe variance | seed mean variance |',
             '| ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: |']
    for (n, alpha, k, kind), stat in sorted(scale.items()):
        lines.append(f"| {n} | {alpha} | {k} | {kind} | {stat['samples']} | {stat['seeds']} | {stat['mean']:.3f} | {stat['variance']:.3f} | {stat['seed_variance']:.3f} |")
    lines += ['', '![Reliability](reliability.svg)', '',
              '| N | alpha | k | loss | delay ms | type | samples | seeds | success rate | success variance | seed rate variance |',
              '| ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: |']
    for (n, alpha, k, probability, latency, kind), stat in sorted(loss.items()):
        lines.append(f"| {n} | {alpha} | {k} | {probability:g} | {latency:g} | {kind} | {stat['samples']} | {stat['seeds']} | {stat['mean']:.3f} | {stat['variance']:.3f} | {stat['seed_variance']:.3f} |")
    lines += ['',
              'Expected node lookup rounds grow approximately as log2(N) for b=1. The dashed line is a growth reference, not a prediction of exact probe counts: parallel batches, k shortlist verification and value cache hits change the constant and can dominate these network sizes.', '',
              'Packet loss applies independently to requests and replies. With three attempts, one isolated RPC has idealized success probability 1-(1-(1-p)^2)^3. A lookup depends on several RPCs and alternate routes, so its success rate need not equal this expression. More loss should lower reliability and increase timeout/probe cost.', '',
              'The setup uses actual bootstrap joins. Seeded addresses, keys and loss draws make inputs repeatable; concurrent scheduling changes which packet receives a loss draw and prevents identical traces. Maintenance is disabled during measurements to isolate lookup behavior. Churn repair has a separate integration test.', '']
    (root / 'analysis.md').write_text('\n'.join(lines), encoding='utf-8')
    print(f'Wrote plots and analysis to {root}')


if __name__ == '__main__':
    main()

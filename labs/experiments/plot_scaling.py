"""Generate the lookup scaling figure and its quantitative comparison."""
import csv
import math
import os
import sys
from pathlib import Path

base = Path(__file__).resolve().parent
os.environ.setdefault('OPENBLAS_NUM_THREADS', '1')
sys.path.insert(0, str(base / '.plot-deps'))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt


def fit(x, y):
    """OLS with intercept; a descriptive fit, not a theoretical prediction."""
    xm, ym = sum(x) / len(x), sum(y) / len(y)
    denominator = sum((v - xm) ** 2 for v in x)
    slope = sum((a - xm) * (b - ym) for a, b in zip(x, y)) / denominator if denominator else 0
    intercept = ym - slope * xm
    residual = sum((b - intercept - slope * a) ** 2 for a, b in zip(x, y))
    total = sum((v - ym) ** 2 for v in y)
    return intercept, slope, math.sqrt(residual / len(y)), 1 - residual / total if total else 1


def main():
    with (base / 'lookup_summary.csv').open(newline='') as source:
        rows = list(csv.DictReader(source))
    if not rows or 'avg_rounds' not in rows[0]:
        raise SystemExit('Rerun the updated experiment first; measured rounds are required.')

    fig, axes = plt.subplots(1, 2, figsize=(11, 4.8))
    comparisons = []
    for column, kind in enumerate(('node', 'value')):
        scope = 'all' if kind == 'node' else 'remote'
        subset = [r for r in rows if r['lookup_type'] == kind and r['scope'] == scope]
        groups = sorted({(int(r['alpha']), int(r['k'])) for r in subset})
        displayed = (3, 10) if (3, 10) in groups else groups[0]
        for alpha, k in groups:
            group = sorted((r for r in subset if int(r['alpha']) == alpha and int(r['k']) == k),
                           key=lambda r: int(r['node_count']))
            x = [int(r['node_count']) for r in group]
            for metric, factor in (('probes', alpha), ('rounds', 1)):
                y = [float(r[f'avg_{metric}']) for r in group]
                reference = [y[0] + factor * math.log2(n / x[0]) for n in x]
                if metric == 'probes' and (alpha, k) == displayed:
                    ax = axes[column]
                    ax.plot(x, y, color='#2563eb', marker='o', linewidth=2,
                            label='Actual tested')
                    ax.plot(x, reference, color='#ea580c', linestyle='--', linewidth=2,
                            label='Expected logarithmic growth')
                intercept, slope, rmse, r2 = fit([math.log2(n) for n in x], y)
                li, ls, le, lr = fit(x, y)
                comparisons.append(dict(lookup_type=kind, scope=scope, alpha=alpha, k=k, metric=metric,
                                       log_intercept=intercept, log_slope=slope, log_rmse=rmse, log_r2=r2,
                                       linear_intercept=li, linear_slope=ls, linear_rmse=le, linear_r2=lr))
        ax = axes[column]
        ax.set_title(f'{kind.capitalize()} lookup ($\\alpha={displayed[0]}, k={displayed[1]}$)')
        ax.set_xlabel('Network size, N (nodes)')
        ax.set_ylabel('Mean probes per lookup')
        ax.set_xticks(sorted({int(r['node_count']) for r in subset}))
        ax.tick_params(axis='x', labelsize=8)
        ax.set_ylim(bottom=0)
        ax.grid(alpha=0.2)
        ax.legend(fontsize=9)

    fig.suptitle('Kademlia lookup probes: expected and actual', fontsize=14)
    fig.text(0.5, 0.015,
             'Expected: anchored logarithmic reference. Value lookup excludes local reads.',
             ha='center', fontsize=8)
    fig.tight_layout(rect=(0, 0.055, 1, 0.94))
    for extension in ('png', 'svg', 'pdf'):
        destination = base / f'lookup_probes_vs_n.{extension}'
        fig.savefig(destination, dpi=200)
        if extension == 'svg':
            destination.write_text('\n'.join(line.rstrip() for line in destination.read_text().splitlines()) + '\n')

    with (base / 'lookup_scaling_comparison.csv').open('w', newline='') as output:
        writer = csv.DictWriter(output, fieldnames=list(comparisons[0]))
        writer.writeheader()
        writer.writerows(comparisons)

    report_path = base / 'report.md'
    report = report_path.read_text(encoding='utf-8').split('\n## Quantitative scaling comparison')[0]
    report += '\n## Quantitative scaling comparison\n\n'
    report += ('Both fits have two coefficients. They describe this finite range; neither establishes asymptotic complexity. '
               'Fits use equally weighted configuration means. RMSE is in probes or measured rounds. '
               'The anchored reference assumes one extra discovery round per doubling, rather than a universal Kademlia constant.\n\n')
    report += '| type | scope | alpha | k | metric | log slope | log RMSE | log R² | linear RMSE | linear R² |\n'
    report += '|---|---|---:|---:|---|---:|---:|---:|---:|---:|\n'
    for c in comparisons:
        report += (f"| {c['lookup_type']} | {c['scope']} | {c['alpha']} | {c['k']} | {c['metric']} | "
                   f"{c['log_slope']:.4f} | {c['log_rmse']:.4f} | {c['log_r2']:.4f} | "
                   f"{c['linear_rmse']:.4f} | {c['linear_r2']:.4f} |\n")
    report += '\nThe simplified figure shows expected and actual probes at alpha=3, k=10 when available. Other configurations, measured rounds and uncertainty remain in the CSV tables above.\n'
    report += '\n![Expected and actual lookup probes](lookup_probes_vs_n.png)\n'
    report_path.write_text(report, encoding='utf-8')
    print('Created PNG, SVG, PDF, lookup_scaling_comparison.csv and report comparison.')


if __name__ == '__main__':
    main()

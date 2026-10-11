"""Plot Experiment 1 summary data; run with python labs/experiments/plot_lookup.py."""
import csv
import sys
from pathlib import Path

base = Path(__file__).resolve().parent
sys.path.insert(0, str(base / '.plot-deps'))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

with (base / 'lookup_summary.csv').open(newline='') as source:
    rows = list(csv.DictReader(source))

fig, axes = plt.subplots(1, 2, figsize=(10, 4.5), sharey=True)
for ax, lookup_type in zip(axes, ('node', 'value')):
    subset = [r for r in rows if r['lookup_type'] == lookup_type]
    groups = sorted({(int(r['alpha']), int(r['k'])) for r in subset})
    for index, (alpha, k) in enumerate(groups):
        points = sorted((int(r['node_count']), float(r['avg_probes']))
                        for r in subset if int(r['alpha']) == alpha and int(r['k']) == k)
        x, y = zip(*points)
        ax.plot(x, y, marker=('o', 's', '^')[index % 3],
                linestyle=('-', '--', ':')[index % 3], linewidth=2,
                markersize=7, markerfacecolor='none' if index else None,
                label=fr'$\alpha={alpha},\ K={k}$')
    ax.set_title(f'{lookup_type.capitalize()} lookup')
    ax.set_xlabel('Network size, N (nodes)')
    ax.set_xticks(sorted({int(r['node_count']) for r in subset}))
    ax.grid(alpha=0.25)
    ax.legend()
axes[0].set_ylabel('Mean probes per lookup')
axes[0].set_ylim(bottom=0)
fig.suptitle('Experiment 1: Lookup probes versus network size')
sample_counts = sorted({int(r['samples']) for r in rows})
sample_label = '/'.join(str(count) for count in sample_counts)
fig.text(0.5, 0.02, f'{sample_label} lookups per point | Pre-populated routing tables | Value means include local reads',
         ha='center', fontsize=9)
fig.tight_layout(rect=(0, 0.06, 1, 0.94))
for extension in ('png', 'svg', 'pdf'):
    fig.savefig(base / f'lookup_probes_vs_n.{extension}', dpi=200)
print('Created lookup_probes_vs_n.png, .svg, .pdf')

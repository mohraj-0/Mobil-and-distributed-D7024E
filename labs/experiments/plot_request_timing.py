"""Experiment 3 timing figures, with only expected and tested curves."""
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

directory = Path(sys.argv[1]) if len(sys.argv) > 1 else base / 'request_response'
with (directory / 'summary.csv').open(newline='') as source:
    rows = list(csv.DictReader(source))
configurations = sorted({(int(r['node_count']), int(r['alpha']), int(r['k'])) for r in rows})
if not configurations:
    raise SystemExit('Run Experiment 3 first.')
displayed = (1000, 3, 10) if (1000, 3, 10) in configurations else configurations[0]
rows = [r for r in rows if (int(r['node_count']), int(r['alpha']), int(r['k'])) == displayed]


def draw(ax, group, x_field, actual, expected, xlabel, title, ylabel):
    group = sorted(group, key=lambda r: float(r[x_field]))
    multiplier = 100 if x_field == 'loss' else 1
    x = [multiplier * float(r[x_field]) for r in group]
    number = lambda value: float(value) if value else math.nan
    ax.plot(x, [number(r[actual]) for r in group], color='#2563eb',
            marker='o', linewidth=2, label='Actual tested')
    ax.plot(x, [number(r[expected]) for r in group], color='#ea580c',
            linestyle='--', linewidth=2, label='Expected')
    ax.set_title(title)
    ax.set_xlabel(xlabel)
    ax.set_ylabel(ylabel)
    ax.set_xticks(x)
    ax.set_ylim(bottom=0)
    ax.grid(alpha=0.2)
    ax.legend(fontsize=9)


def save(fig, name):
    for extension in ('png', 'svg', 'pdf'):
        destination = directory / f'{name}.{extension}'
        fig.savefig(destination, dpi=200)
        if extension == 'svg':
            destination.write_text('\n'.join(line.rstrip() for line in destination.read_text().splitlines()) + '\n')


fig, axes = plt.subplots(2, 2, figsize=(11, 8.5), sharey='row')
for column, kind in enumerate(('node', 'value')):
    for row, axis in enumerate(('latency', 'loss')):
        group = [r for r in rows if r['axis'] == axis and r['lookup_type'] == kind]
        x_field = 'latency_ms' if axis == 'latency' else 'loss'
        xlabel = 'One-way latency (ms)' if axis == 'latency' else 'Packet loss (%)'
        detail = 'zero packet loss' if axis == 'latency' else f"{float(group[0]['latency_ms']):g} ms one-way latency"
        draw(axes[row, column], group, x_field, 'avg_response_ms', 'expected_response_ms',
             xlabel, f'{kind.capitalize()} lookup — {detail}', 'Mean on-time reply RTT (ms)')
fig.suptitle(fr'Experiment 3: Request–response time ($N={displayed[0]},\ \alpha={displayed[1]},\ k={displayed[2]}$)', fontsize=14)
fig.text(0.5, 0.015,
         'Expected RTT = twice the one-way delay. Only replies received before the deadline contribute; missing means are gaps.',
         ha='center', fontsize=8)
fig.tight_layout(rect=(0, 0.04, 1, 0.95))
save(fig, 'request_response_time')

fig, axes = plt.subplots(1, 2, figsize=(11, 4.8), sharey=True)
for ax, kind in zip(axes, ('node', 'value')):
    group = [r for r in rows if r['axis'] == 'loss' and r['lookup_type'] == kind]
    draw(ax, group, 'loss', 'avg_outcome_wait_ms', 'expected_outcome_wait_ms',
         'Packet loss (%)', f'{kind.capitalize()} lookup', 'Mean request outcome wait (ms)')
fig.suptitle('Experiment 3: Request wait including missing replies', fontsize=14)
fig.text(0.5, 0.015,
         'On-time reply: measured RTT. Missing or late reply: configured deadline. This is not whole-lookup duration.',
         ha='center', fontsize=8)
fig.tight_layout(rect=(0, 0.055, 1, 0.94))
save(fig, 'request_wait_vs_loss')
print('Created Experiment 3 RTT and timeout-inclusive wait figures as PNG, SVG and PDF.')

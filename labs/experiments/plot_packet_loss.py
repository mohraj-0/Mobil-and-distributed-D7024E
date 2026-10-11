"""Experiment 2: only expected and actual lookup reliability curves."""
import csv
import os
import sys
from pathlib import Path

base = Path(__file__).resolve().parent
os.environ.setdefault('OPENBLAS_NUM_THREADS', '1')
sys.path.insert(0, str(base / '.plot-deps'))
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

directory = Path(sys.argv[1]) if len(sys.argv) > 1 else base / 'packet_loss'
with (directory / 'summary.csv').open(newline='') as source:
    rows = list(csv.DictReader(source))
configurations = sorted({(int(r['node_count']), int(r['alpha']), int(r['k'])) for r in rows})
if not configurations:
    raise SystemExit('Run the packet-loss experiment first.')
displayed = (1000, 3, 10) if (1000, 3, 10) in configurations else configurations[0]
fig, axes = plt.subplots(1, 2, figsize=(11, 4.8), sharey=True)
for ax, kind in zip(axes, ('node', 'value')):
    group = sorted((r for r in rows if r['lookup_type'] == kind
                    and (int(r['node_count']), int(r['alpha']), int(r['k'])) == displayed),
                   key=lambda r: float(r['loss']))
    x = [100 * float(r['loss']) for r in group]
    ax.plot(x, [100 * float(r['success_rate']) for r in group],
            color='#2563eb', marker='o', linewidth=2, label='Actual tested')
    ax.plot(x, [100 * float(r['expected_success']) for r in group],
            color='#ea580c', linestyle='--', linewidth=2, label='Expected (ideal reachability)')
    ax.set_title(f'{kind.capitalize()} lookup')
    ax.set_xlabel('Packet loss (%)')
    ax.set_ylabel('Successful lookups (%)')
    ax.set_xticks(x)
    ax.set_ylim(-2, 102)
    ax.grid(alpha=0.2)
    ax.legend(fontsize=9)
fig.suptitle(fr'Experiment 2: Lookup reliability ($N={displayed[0]},\ \alpha={displayed[1]},\ k={displayed[2]}$)', fontsize=14)
fig.text(0.5, 0.015,
         'Loss applies independently to requests and replies. Expected curves assume direct reachability. Value reads are remote.',
         ha='center', fontsize=8)
fig.tight_layout(rect=(0, 0.055, 1, 0.94))
for extension in ('png', 'svg', 'pdf'):
    destination = directory / f'lookup_reliability_vs_loss.{extension}'
    fig.savefig(destination, dpi=200)
    if extension == 'svg':
        destination.write_text('\n'.join(line.rstrip() for line in destination.read_text().splitlines()) + '\n')
print('Created Experiment 2 PNG, SVG and PDF.')

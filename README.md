# kintsugi

Repair poisoned shared memory in a multi-agent system. When a source turns out to be bad, kintsugi walks a provenance graph in FalkorDB to find everything that depended on it across agent handoffs, re-runs only those actions against clean memory, stops early where a re-run gives the same result, and lists external effects that cannot be undone.

Work in progress.

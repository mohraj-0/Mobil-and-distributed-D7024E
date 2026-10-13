# CLI test guides

Run the commands from `labs`, following the guide for your network:

- [UDP without Docker](CLI-UDP-GUIDE.md)
- [UDP with Docker](CLI-DOCKER-GUIDE.md)
- [Simulated network](CLI-SIMULATED-GUIDE.md)

Each guide includes setup, Part 1 and Part 2 tests, ten versions of one file,
invalid updates, explanations, and expected results.

The UDP guides split writes/publications and reads/installs across independent
client terminals on the same network, with separate client ports. The simulated
guide explains why its second terminal can only manage files: separate simulated
processes do not share an in-memory network. Use local UDP for communication
between terminals without Docker.

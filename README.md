# OneClick Dev Server

A Windows desktop application for safely running and publishing untrusted local web projects through an isolated Hyper-V virtual machine.

> Status: initial scaffold in progress.

## Security goal

Untrusted application code must never be executed directly on the Windows host. The desktop app acts only as an orchestrator; workloads run inside an isolated VM and are exposed through a tunnel from inside that VM.

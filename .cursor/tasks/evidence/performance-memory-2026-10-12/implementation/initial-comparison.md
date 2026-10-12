# Baseline / candidate benchmark comparison

Five samples each, same Go/OS/arch; median values. CPU gate uses paired repeat when recorded. Copy-control and direct conversion are unchanged controls; only full recording and scope exercise implementation differences.

| Path | Base ns | Candidate ns | Ratio | Base B / allocs | Candidate B / allocs | Gate CPU ratio |
|---|---:|---:|---:|---:|---:|---:|
| BenchmarkPerformanceLabels/copy_control/0 | 2.744 | 2.743 | 1.000 | 0 / 0 | 0 / 0 | 1.000 |
| BenchmarkPerformanceLabels/copy_control/256 | 12483 | 11971 | 0.959 | 36904 / 4 | 36904 / 4 | 0.959 |
| BenchmarkPerformanceLabels/copy_control/64 | 3248 | 3180 | 0.979 | 9768 / 4 | 9768 / 4 | 0.979 |
| BenchmarkPerformanceLabels/copy_control/8 | 319.8 | 315.3 | 0.986 | 512 / 1 | 512 / 1 | 0.986 |
| BenchmarkPerformanceLabels/direct/0 | 2.235 | 3.913 | 1.751 | 0 / 0 | 0 / 0 | 1.751 |
| BenchmarkPerformanceLabels/direct/256 | 5205 | 5069 | 0.974 | 18432 / 1 | 18432 / 1 | 0.974 |
| BenchmarkPerformanceLabels/direct/64 | 1432 | 1332 | 0.930 | 4864 / 1 | 4864 / 1 | 0.930 |
| BenchmarkPerformanceLabels/direct/8 | 202.5 | 206.8 | 1.021 | 512 / 1 | 512 / 1 | 1.021 |
| BenchmarkPerformanceRecording/disabled/counter/0 | 3.071 | 10.88 | 3.543 | 0 / 0 | 0 / 0 | 3.543 |
| BenchmarkPerformanceRecording/disabled/counter/256 | 3.069 | 3.035 | 0.989 | 0 / 0 | 0 / 0 | 0.989 |
| BenchmarkPerformanceRecording/disabled/counter/64 | 3.034 | 5.629 | 1.855 | 0 / 0 | 0 / 0 | 1.855 |
| BenchmarkPerformanceRecording/disabled/counter/8 | 3.051 | 11.39 | 3.733 | 0 / 0 | 0 / 0 | 3.733 |
| BenchmarkPerformanceRecording/disabled/gauge/0 | 2.856 | 2.863 | 1.002 | 0 / 0 | 0 / 0 | 1.002 |
| BenchmarkPerformanceRecording/disabled/gauge/256 | 2.852 | 6.755 | 2.369 | 0 / 0 | 0 / 0 | 2.369 |
| BenchmarkPerformanceRecording/disabled/gauge/64 | 2.866 | 2.971 | 1.037 | 0 / 0 | 0 / 0 | 1.037 |
| BenchmarkPerformanceRecording/disabled/gauge/8 | 2.847 | 2.881 | 1.012 | 0 / 0 | 0 / 0 | 1.012 |
| BenchmarkPerformanceRecording/disabled/histogram/0 | 2.987 | 7.51 | 2.514 | 0 / 0 | 0 / 0 | 2.514 |
| BenchmarkPerformanceRecording/disabled/histogram/256 | 2.936 | 3 | 1.022 | 0 / 0 | 0 / 0 | 1.022 |
| BenchmarkPerformanceRecording/disabled/histogram/64 | 2.925 | 3.007 | 1.028 | 0 / 0 | 0 / 0 | 1.028 |
| BenchmarkPerformanceRecording/disabled/histogram/8 | 2.931 | 4.152 | 1.417 | 0 / 0 | 0 / 0 | 1.417 |
| BenchmarkPerformanceRecording/memory/counter/0 | 362 | 91.51 | 0.253 | 40 / 2 | 40 / 2 | 0.253 |
| BenchmarkPerformanceRecording/memory/counter/256 | 552619 | 67753 | 0.123 | 92243 / 9 | 73768 / 6 | 0.123 |
| BenchmarkPerformanceRecording/memory/counter/64 | 141878 | 13026 | 0.092 | 24400 / 9 | 19496 / 6 | 0.092 |
| BenchmarkPerformanceRecording/memory/counter/8 | 10177 | 1066 | 0.105 | 1576 / 5 | 1576 / 5 | 0.105 |
| BenchmarkPerformanceRecording/memory/gauge/0 | 205.1 | 90.5 | 0.441 | 40 / 2 | 40 / 2 | 0.441 |
| BenchmarkPerformanceRecording/memory/gauge/256 | 129148 | 177280 | 1.373 | 92241 / 9 | 73769 / 6 | 1.373 |
| BenchmarkPerformanceRecording/memory/gauge/64 | 31881 | 34789 | 1.091 | 24400 / 9 | 19496 / 6 | 1.091 |
| BenchmarkPerformanceRecording/memory/gauge/8 | 3209 | 8309 | 2.589 | 1576 / 5 | 1576 / 5 | 2.589 |
| BenchmarkPerformanceRecording/memory/histogram/0 | 285.7 | 107.6 | 0.377 | 40 / 2 | 40 / 2 | 0.377 |
| BenchmarkPerformanceRecording/memory/histogram/256 | 169496 | 67561 | 0.399 | 92242 / 9 | 73768 / 6 | 0.399 |
| BenchmarkPerformanceRecording/memory/histogram/64 | 19148 | 13202 | 0.689 | 24400 / 9 | 19496 / 6 | 0.689 |
| BenchmarkPerformanceRecording/memory/histogram/8 | 3113 | 1088 | 0.350 | 1576 / 5 | 1576 / 5 | 0.350 |
| BenchmarkPerformanceRecording/noop/counter/0 | 33.29 | 32.16 | 0.966 | 40 / 2 | 40 / 2 | 0.966 |
| BenchmarkPerformanceRecording/noop/counter/256 | 71449 | 61251 | 0.857 | 92240 / 9 | 73768 / 6 | 0.857 |
| BenchmarkPerformanceRecording/noop/counter/64 | 14445 | 11542 | 0.799 | 24400 / 9 | 19496 / 6 | 0.799 |
| BenchmarkPerformanceRecording/noop/counter/8 | 933.8 | 779.8 | 0.835 | 1576 / 5 | 1576 / 5 | 0.835 |
| BenchmarkPerformanceRecording/noop/gauge/0 | 36.71 | 32.85 | 0.895 | 40 / 2 | 40 / 2 | 0.895 |
| BenchmarkPerformanceRecording/noop/gauge/256 | 536115 | 60912 | 0.114 | 92244 / 9 | 73768 / 6 | 0.114 |
| BenchmarkPerformanceRecording/noop/gauge/64 | 18311 | 11611 | 0.634 | 24400 / 9 | 19496 / 6 | 0.634 |
| BenchmarkPerformanceRecording/noop/gauge/8 | 1130 | 793.5 | 0.702 | 1576 / 5 | 1576 / 5 | 0.702 |
| BenchmarkPerformanceRecording/noop/histogram/0 | 33.76 | 33.06 | 0.979 | 40 / 2 | 40 / 2 | 0.979 |
| BenchmarkPerformanceRecording/noop/histogram/256 | 78760 | 61803 | 0.785 | 92240 / 9 | 73768 / 6 | 0.785 |
| BenchmarkPerformanceRecording/noop/histogram/64 | 18796 | 11484 | 0.611 | 24400 / 9 | 19496 / 6 | 0.611 |
| BenchmarkPerformanceRecording/noop/histogram/8 | 949.6 | 792.4 | 0.834 | 1576 / 5 | 1576 / 5 | 0.834 |
| BenchmarkPerformanceScope/complete/0 | 40.63 | 15.04 | 0.370 | 0 / 0 | 0 / 0 | 0.370 |
| BenchmarkPerformanceScope/complete/256 | 22868 | 13.89 | 0.001 | 65536 / 4 | 0 / 0 | 0.001 |
| BenchmarkPerformanceScope/complete/64 | 7432 | 20.61 | 0.003 | 16384 / 4 | 0 / 0 | 0.003 |
| BenchmarkPerformanceScope/complete/8 | 567.8 | 16.64 | 0.029 | 1792 / 4 | 0 / 0 | 0.029 |
| BenchmarkPerformanceScope/partial/0 | 346.8 | 38.38 | 0.111 | 896 / 4 | 0 / 0 | 0.111 |
| BenchmarkPerformanceScope/partial/256 | 27884 | 466.6 | 0.017 | 65537 / 4 | 0 / 0 | 0.017 |
| BenchmarkPerformanceScope/partial/64 | 41743 | 201.8 | 0.005 | 16384 / 4 | 0 / 0 | 0.005 |
| BenchmarkPerformanceScope/partial/8 | 826.5 | 87.62 | 0.106 | 2816 / 4 | 0 / 0 | 0.106 |
| BenchmarkPerformanceScope/string/0 | 85.09 | 19.35 | 0.227 | 224 / 1 | 0 / 0 | 0.227 |
| BenchmarkPerformanceScope/string/256 | 4566 | 169.6 | 0.037 | 16384 / 1 | 0 / 0 | 0.037 |
| BenchmarkPerformanceScope/string/64 | 2706 | 57.03 | 0.021 | 4096 / 1 | 0 / 0 | 0.021 |
| BenchmarkPerformanceScope/string/8 | 217.3 | 23.09 | 0.106 | 704 / 1 | 0 / 0 | 0.106 |

Failures: BenchmarkPerformanceLabels/direct/0: CPU regression >10%; BenchmarkPerformanceRecording/disabled/counter/0: CPU regression >10%; BenchmarkPerformanceRecording/disabled/counter/64: CPU regression >10%; BenchmarkPerformanceRecording/disabled/counter/8: CPU regression >10%; BenchmarkPerformanceRecording/disabled/gauge/256: CPU regression >10%; BenchmarkPerformanceRecording/disabled/histogram/0: CPU regression >10%; BenchmarkPerformanceRecording/disabled/histogram/8: CPU regression >10%; BenchmarkPerformanceRecording/memory/gauge/256: CPU regression >10%; BenchmarkPerformanceRecording/memory/gauge/8: CPU regression >10%

# Explainer animation

Source for the ~55 s explainer video embedded at the top of the repo README.
It is drawn from code (pycairo + ffmpeg), so labels are exact and it can be
re-rendered when the project changes.

## Render

```sh
pip install pycairo          # plus ffmpeg and the Inter font on the system
python3 render.py preview    # stills of key moments in out/
python3 render.py render     # out/market-data-platform-explainer.mp4
```

Outputs go to `out/`, which is gitignored. The video itself is hosted as a
GitHub attachment, not committed, so re-renders do not grow the repo history.

## Updating

- Scene timing and captions: `SCENES` in `render.py`.
- Built vs next lists: `end_frame`.
- The chaos scene shows "0 messages lost" as a goal. Replace it with measured
  RTO / data-loss numbers only after the Phase 6 runs are reproduced twice.

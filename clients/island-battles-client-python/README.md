# Island Battles Python client (retired scaffold)

This directory is an incomplete historical scaffold. It does not implement
the generic lobby authentication, retained guest sessions, lease lifecycle, or
realtime lobby protocol.

Use the maintained client in [`../lobby-python/`](../lobby-python/) instead.
Its generated HTTP SDK and handwritten asyncio wrapper follow the Huma lobby
schema and provide the runnable terminal example. The lobby client only
coordinates lobby membership; it does not invoke Lambda or generate Island
Battles maps/simulation.

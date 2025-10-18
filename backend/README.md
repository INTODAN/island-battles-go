The backend for the island battles project.  
Meant to be run as a docker container in a serverless architecture.

Uses postgresql for keeping a state, and centrifugo for keeping realtime connections.  
The backend is language agnostic, and is used for all clients.  

To run centrifugo locally, download their binary and serve with `centrifugo --config=backend/centrifugo-config.toml`
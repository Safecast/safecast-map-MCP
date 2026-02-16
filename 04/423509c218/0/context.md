# Session Context

**Session ID:** b6847497-f5f3-4b52-a8dd-39eafaa75c69

**Commit Message:** For error on github action deploy:

## Prompt

For error on github action deploy:

## Summary

Perfect! I've made the health check more robust:

1. **Increased initial wait**: From 3 to 10 seconds
2. **Added retry logic**: Will try up to 5 times with 5-second delays between attempts
3. **Total wait time**: Up to ~35 seconds for the server to fully start

This should handle the case where the server takes longer to initialize after restart. The changes are in [.github/workflows/deploy.yml](. github/workflows/deploy.yml#L63-L89).

## Key Actions

- - **Read**: /home/rob/Documents/Safecast/safecast-map-MCP/.github/workflows/deploy.yml
- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/.github/workflows/deploy.yml
- - **Edit**: /home/rob/Documents/Safecast/safecast-map-MCP/.github/workflows/deploy.yml

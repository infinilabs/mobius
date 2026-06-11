---
name: playable-publishing
description: Publish playable ads to cloud bucket storage.
---

# Playable Ad Publishing Skill

You are Playable Publisher. Your task is to upload the playable build to the cloud and deliver the final signed URL.

## Workflow Steps
1. Retrieve pipeline ID from PM.
2. Call `publish_playable_ad` tool with pipeline ID.
3. The tool uploads the self-contained playable assets to GCS bucket storage.
4. Verify the tool returns a signed production URL.
5. Report the final URL to Linas/Elong.

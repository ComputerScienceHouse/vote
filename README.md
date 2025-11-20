# vote

because paper ballots are so 2019

Implementation 

- **Server-side rendering**. That's right, this site (should) (mostly) work without JavaScript.
- **Server Sent Events** for real-time vote results
- **~~Limited~~ voting options**. It's now just as good as Google Forms, but a lot less safe! That's what you get when a bored college student does this in their free time

## Configuration

You'll need to set up these values in your environment. Ask an RTP for OIDC credentials. A docker-compose file is provided for convenience. Otherwise, I trust you to figure it out!

```
VOTE_HOST=http://localhost:8080
VOTE_JWT_SECRET=
VOTE_MONGODB_URI=
VOTE_OIDC_ID=
VOTE_OIDC_SECRET=
VOTE_STATE=
VOTE_TOKEN=
VOTE_CONDITIONAL_URL=https://conditional.csh.rit.edu/gatekeep/
VOTE_ANNOUNCEMENTS_CHANNEL_ID=
VOTE_SLACK_APP_TOKEN=
VOTE_SLACK_BOT_TOKEN=
```

## To-Dos

- [ ] Don't let the user fuck it up
- [ ] Show E-Board polls with a higher priority
- [ ] Display the reason why a user is on the results page of a running poll
- [ ] Display minimum time left that a poll is open
# vote
because paper ballots are so 2019

Imagine this. You're a somehow still functioning student organization of computer nerds. You've been using paper ballots to vote for the last 40 years. But then, disaster strikes! Global ~~ligma~~ COVID takes over, and if you so much look at a slip of paper, The Virus will take you. Enter vote, a 🚀 blazingly fast 🚀... Wait. This is Go, not Rust. It can't be blazingly fast. Uhh... Enter vote, a reasonably fast voting app with less memory safety than if it was written in Rust. But hey, gotta Pokemon _Go_ to the polls somehow, right? Right...? This is why I'm a software engineer and not a comedian.

Anyways, now we can vote online. It's cool, I guess? We have things such as:
 - **Server-side rendering**. That's right, this site (should) (mostly) work without JavaScript.
 - **Server Sent Events** for real-time vote results
 - **~~Limited~~ voting options**. It's now just as good as Google Forms, but a lot less safe! That's what you get when a bored college student does this in their free time

## Configuration
You'll need to set up these values in your environment. Ask an RTP for OIDC credentials. A docker-compose file is provided for convenience. Otherwise, I trust you to figure it out!
```
VOTE_HOST=http://localhost:8080
VOTE_JWT_SECRET=
VOTE_MONGO_DB=
VOTE_MONGODB_URI=
VOTE_OIDC_ID=vote
VOTE_OIDC_SECRET=
VOTE_STATE=
```

## To-Dos
- [x] Custom vote options
- [x] Write-in votes
- [x] Ranked choice voting
- [x] Show options that got no votes
- [x] Allow results to be hidden until a vote is closed
- [ ] Don't let the user fuck it up
- [ ] Show eboard polls with a higher priority

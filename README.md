# vote

because paper ballots are so 2019

Implementation 

- **Server-side rendering**. That's right, this site (should) (mostly) work without JavaScript.
- **Server Sent Events** for real-time vote results
- **~~Limited~~ voting options**. It's now just as good as Google Forms, but a lot less safe! That's what you get when a bored college student does this in their free time
- **Constitutional Vote Mode**. This is an exclusive feature to Evals. It ensures votes meet quorum, and then automatically close if they do. If they do not meet quorum, it DMs all people eligible to vote who have not.

## Configuration

Copy `.env.example` to `.env` and fill out the info

### Dev Overrides
`DEV_DISABLE_ACTIVE_FILTERS="true"` will disable the requirements that you be active to vote
`DEV_FORCE_IS_EBOARD="true"` will force vote to treat all users as E-Board members
`DEV_FORCE_IS_EVALS="true"` will force vote to treat all users as the Evals director

## Linting
These will be checked by CI

```
# tidy dependencies
go mod tidy

# format all code according to go standards
gofmt -w -s *.go logging sse database

# run tests (database is the first place we've defined tests)
go test ./database

# run heuristic validation
go vet ./database/ ./logging/ ./sse/
go vet *.go
```

## To-Dos

- [ ] Don't let the user fuck it up
- [ ] Show E-Board polls with a higher priority
- [x] Move Hide Vote to create instead of after you vote :skull:
- [X] Display the reason why a user is on the results page of a running poll
- [ ] Display minimum time left that a poll is open
- [ ] Move routes to their own functions 
- [X] Change HTTP resposne codes to be `http.something` instead of just a number

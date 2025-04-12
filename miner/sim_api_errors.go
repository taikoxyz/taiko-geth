package miner

type HaltError struct {
	err error
}

type RetrieveEnError struct {
	err error
}

type CommitError struct {
	err error
}

type RevertCommitError struct {
	err error
}

func NewHaltError(err error) HaltError {
	return HaltError{err: err}
}
func (e HaltError) Error() string {
	return e.err.Error()
}

func NewRevertCommitError(err error) RevertCommitError {
	return RevertCommitError{err: err}
}

func (e RevertCommitError) Error() string {
	return e.err.Error()
}

func NewRetrieveEnError(err error) RetrieveEnError {
	return RetrieveEnError{err: err}
}

func (e RetrieveEnError) Error() string {
	return e.err.Error()
}

func NewCommitError(err error) *CommitError {
	return &CommitError{err: err}
}

func (e CommitError) Error() string {
	return e.err.Error()
}

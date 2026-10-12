package service

// Install creates the logon task and starts it now. There is no restart on
// crash: scheduled tasks don't supervise a detached process.
func Install(o Options, e Env) error {
	if err := e.run("schtasks", SchtasksCreateArgs(o)...); err != nil {
		return err
	}
	return e.run("schtasks", "/Run", "/TN", Name)
}

// Uninstall deletes the task. A running agent keeps running until it exits.
func Uninstall(e Env) error {
	return e.run("schtasks", "/Delete", "/F", "/TN", Name)
}

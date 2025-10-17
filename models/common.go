package models

// DomainProfile defines the VM hardware profile for building and running the sandbox.
type DomainProfile struct {
	Arch         string
	Machine      *string
	CPUModel     *string
	VCPUs        int
	RAMMB        int
	DiskBus      string
	DiskTarget   string
	CDBus        string
	CDPrefix     string
	SetupLetter  string
	SampleLetter string
	NetworkModel string
	ExtraArgs    []string
}

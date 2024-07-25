package packages

import "package-operator.run/internal/packages/internal/packagekickstart"

type KickstartResult = packagekickstart.KickstartResult

var (
	Kickstart            = packagekickstart.Kickstart
	KickstartFromBytes   = packagekickstart.KickstartFromBytes
	ImportOLMBundleImage = packagekickstart.ImportOLMBundleImage
)

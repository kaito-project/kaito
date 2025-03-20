package v1alpha1

// Map Representing Minimum Per GPU Memory required for Batch Size of 1
// ModelName, TuningMethod, MinGPUMemory
var modelTuningConfigs = map[string]map[string]int{
	"falcon-7b": {
		//string(TuningMethodLora): 24,
		string(TuningMethodQLora): 16,
	},
	// Add more configurations as needed
}

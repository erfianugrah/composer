package dto

type ContainerIDInput struct {
	ID string `path:"id" doc:"Container ID (short 12-char)"`
}

type ContainerListOutput struct {
	Body struct {
		Containers []ContainerOutput `json:"containers"`
	}
}

type ContainerDetailOutput struct {
	Body ContainerOutput
}

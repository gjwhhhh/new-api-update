package authz

const ResourceMail = "mail"

var (
	MailRead    = Permission{Resource: ResourceMail, Action: ActionRead}
	MailWrite   = Permission{Resource: ResourceMail, Action: ActionWrite}
	MailOperate = Permission{Resource: ResourceMail, Action: ActionOperate}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceMail, LabelKey: "Mail Center", Actions: []ActionDefinition{
		{Action: ActionRead, LabelKey: "View email records", DescriptionKey: "View email content and delivery history for all users. Security codes and links are redacted."},
		{Action: ActionWrite, LabelKey: "Send email", DescriptionKey: "Select users, preview, test and send email batches."},
		{Action: ActionOperate, LabelKey: "Manage email batches", DescriptionKey: "Retry failed deliveries and cancel pending email."},
	}})
}

package model

type MockUser struct {
	Id       uint32
	Login    string
	Name     string
	Age      uint8
	ImageUrl string
	Phones   []string
	Emails   []string
	Info     string
}

var users = []MockUser{
	{
		Id:       1,
		Login:    "johndoe123",
		Name:     "John Doe",
		Age:      28,
		ImageUrl: "https://example.com/images/johndoe.jpg",
		Phones:   []string{"+1234567890", "+0987654321"},
		Emails:   []string{"johndoe@example.com", "john.d@example.com"},
		Info:     "John is a highly skilled software engineer with a strong background in full-stack development. Over the last 5 years, he has worked on numerous projects, mastering multiple programming languages including Golang, Python, and JavaScript. His expertise in cloud architecture and microservices has contributed to several successful deployments, making him a key player in the company's growth.",
	},
	{
		Id:       2,
		Login:    "janesmith456",
		Name:     "Jane Smith",
		Age:      34,
		ImageUrl: "https://example.com/images/janesmith.jpg",
		Phones:   []string{"+1122334455"},
		Emails:   []string{"janesmith@example.com"},
		Info:     "Jane is a seasoned product manager with over 8 years of experience in leading cross-functional teams to deliver high-quality products. She has a proven track record of managing complex projects from inception to launch, specializing in mobile and web applications. Jane is adept at utilizing agile methodologies to streamline workflows and boost productivity within her team.",
	},
	{
		Id:       3,
		Login:    "alexjohnson789",
		Name:     "Alex Johnson",
		Age:      26,
		ImageUrl: "https://example.com/images/alexjohnson.jpg",
		Phones:   []string{"+9876543210", "+1029384756"},
		Emails:   []string{"alexjohnson@example.com", "ajohnson@example.com"},
		Info:     "Alex is an enthusiastic data scientist with a passion for machine learning and artificial intelligence. He has a deep understanding of statistical modeling and data analytics, and has applied his knowledge to solve complex problems in various industries. His recent work in natural language processing and predictive analytics has significantly improved customer insights for the company.",
	},
	{
		Id:       4,
		Login:    "michaelbaker321",
		Name:     "Michael Baker",
		Age:      45,
		ImageUrl: "https://example.com/images/michaelbaker.jpg",
		Phones:   []string{"+2233445566"},
		Emails:   []string{"michaelbaker@example.com"},
		Info:     "Michael is an experienced HR manager with a career spanning over 15 years in human resources. He has successfully handled large teams and implemented effective recruitment and retention strategies. His expertise lies in fostering a healthy work environment and addressing employee grievances, ensuring that both the company's needs and employee satisfaction are well-balanced.",
	},
	{
		Id:       5,
		Login:    "emilyclark654",
		Name:     "Emily Clark",
		Age:      30,
		ImageUrl: "https://example.com/images/emilyclark.jpg",
		Phones:   []string{"+5647382910"},
		Emails:   []string{"emilyclark@example.com"},
		Info:     "Emily is a dynamic marketing specialist with a focus on digital marketing strategies. She has successfully led campaigns that increased brand awareness and user engagement for several high-profile clients. Emily is particularly skilled at leveraging social media platforms and SEO techniques to drive organic growth and improve lead generation for businesses.",
	},
	{
		Id:       6,
		Login:    "oliverjames987",
		Name:     "Oliver James",
		Age:      39,
		ImageUrl: "https://example.com/images/oliverjames.jpg",
		Phones:   []string{"+4455667788", "+9988776655"},
		Emails:   []string{"oliverj@example.com"},
		Info:     "Oliver is a DevOps engineer with a decade of experience in automating and streamlining development operations. He is well-versed in CI/CD pipelines, cloud infrastructure, and containerization tools like Docker and Kubernetes. Oliver has led efforts to implement scalable solutions that enhance operational efficiency and reduce downtime across the development lifecycle.",
	},
	{
		Id:       7,
		Login:    "sophiataylor123",
		Name:     "Sophia Taylor",
		Age:      24,
		ImageUrl: "https://example.com/images/sophiataylor.jpg",
		Phones:   []string{"+1010101010"},
		Emails:   []string{"sophia.taylor@example.com"},
		Info:     "Sophia is a junior frontend developer with a passion for creating aesthetically pleasing and highly functional user interfaces. Her attention to detail and understanding of user-centered design principles make her a valuable asset to the team. Sophia has quickly mastered several frontend technologies, including React and Vue.js, and is eager to continue honing her skills in web development.",
	},
	{
		Id:       8,
		Login:    "jackwilliams456",
		Name:     "Jack Williams",
		Age:      31,
		ImageUrl: "https://example.com/images/jackwilliams.jpg",
		Phones:   []string{"+1212121212", "+3434343434"},
		Emails:   []string{"jackw@example.com"},
		Info:     "Jack is a project manager with expertise in Agile methodologies. He has successfully led numerous high-stakes projects, ensuring they are delivered on time and within budget. His excellent communication and leadership skills allow him to coordinate effectively with different departments, ensuring smooth collaboration and achieving project goals.",
	},
	{
		Id:       9,
		Login:    "isabellajohnson789",
		Name:     "Isabella Johnson",
		Age:      29,
		ImageUrl: "https://example.com/images/isabellajohnson.jpg",
		Phones:   []string{"+5656565656"},
		Emails:   []string{"isabella.johnson@example.com"},
		Info:     "Isabella is a UI/UX designer with a deep understanding of user experience and interface design. She has worked on multiple high-profile projects, where her designs have significantly enhanced the user journey and interface usability. Her approach combines creativity with data-driven insights, ensuring that the end product not only looks great but also functions seamlessly.",
	},
	{
		Id:       10,
		Login:    "davidbrown321",
		Name:     "David Brown",
		Age:      42,
		ImageUrl: "https://example.com/images/davidbrown.jpg",
		Phones:   []string{"+6767676767", "+7878787878"},
		Emails:   []string{"david.brown@example.com"},
		Info:     "David is an IT consultant with over 15 years of experience in helping businesses adopt and optimize cloud computing solutions. He specializes in cloud infrastructure, cybersecurity, and data management. His strategic insights have led to significant cost savings and improved operational efficiency for his clients.",
	},
}

var (
	userByLogin map[string]*MockUser
)

func init() {
	userByLogin = make(map[string]*MockUser)
	for i := range users {
		userByLogin[users[i].Login] = &users[i]
	}
}

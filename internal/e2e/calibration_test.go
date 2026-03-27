//go:build e2e

package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Run with:
//   CGO_ENABLED=1 ginkgo -tags e2e -timeout 45m -v ./internal/e2e/
//
// Parallel (each issue gets its own .jorm/ data dir):
//   CGO_ENABLED=1 ginkgo -tags e2e -timeout 20m -v --procs=5 ./internal/e2e/
//
// Single issue:
//   CGO_ENABLED=1 ginkgo -tags e2e -timeout 15m -v -focus "Issue 1" ./internal/e2e/

var _ = Describe("Calibration", func() {

	Describe("Issue 1: StringReverse (TRIVIAL)", Ordered, func() {
		var (
			workDir string
			result  *RunResult
		)

		BeforeAll(func() {
			dir, err := os.MkdirTemp("", "jorm-e2e-issue1-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)

			workDir, err = CloneCalibrationRepo(dir)
			Expect(err).NotTo(HaveOccurred())

			result, err = RunJorm(workDir, "1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should exit successfully", func() {
			Expect(result.ExitCode).To(Equal(0), "jorm output:\n%s", result.Output)
		})

		It("should compile", func() {
			Expect(result.Compiles).To(BeTrue())
		})

		It("should pass all tests", func() {
			Expect(result.TestsPass).To(BeTrue())
		})

		It("should create the Reverse function file", func() {
			Expect(HasFile(workDir, "internal/utils/strings.go")).To(BeTrue())
		})

		It("should create table-driven tests", func() {
			Expect(HasFile(workDir, "internal/utils/strings_test.go")).To(BeTrue())
		})

		It("should include Closes #1 in the commit message", func() {
			msg := CommitMessage(workDir)
			Expect(msg).To(ContainSubstring("Closes #1"))
		})

		It("should complete at least 3 stages", func() {
			completed := CompletedStageNames(result.Stages)
			Expect(len(completed)).To(BeNumerically(">=", 3), "stages: %v", completed)
		})

		Context("post-review", Label("post-review"), func() {
			var pr *PostReviewResult

			BeforeAll(func() {
				var err error
				pr, err = RunPostReview(workDir)
				Expect(err).NotTo(HaveOccurred())
				if pr == nil {
					Skip("post-review script not found")
				}
			})

			It("should pass PR review", func() {
				Expect(pr.PRReview).To(Equal("ACCEPT"), "PR review found issues:\n%s", pr.Output)
			})

			It("should pass security review", func() {
				Expect(pr.Security).To(Equal("ACCEPT"), "Security review found issues:\n%s", pr.Output)
			})
		})
	})

	Describe("Issue 2: Health Check Endpoint (SIMPLE)", Ordered, func() {
		var (
			workDir string
			result  *RunResult
		)

		BeforeAll(func() {
			dir, err := os.MkdirTemp("", "jorm-e2e-issue2-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)

			workDir, err = CloneCalibrationRepo(dir)
			Expect(err).NotTo(HaveOccurred())

			result, err = RunJorm(workDir, "2")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should exit successfully", func() {
			Expect(result.ExitCode).To(Equal(0), "jorm output:\n%s", result.Output)
		})

		It("should compile", func() {
			Expect(result.Compiles).To(BeTrue())
		})

		It("should pass all tests", func() {
			Expect(result.TestsPass).To(BeTrue())
		})

		It("should create the health handler", func() {
			Expect(HasFile(workDir, "internal/handler/health.go")).To(BeTrue())
		})

		It("should create health handler tests", func() {
			Expect(HasFile(workDir, "internal/handler/health_test.go")).To(BeTrue())
		})

		It("should include Closes #2 in the commit message", func() {
			msg := CommitMessage(workDir)
			Expect(msg).To(ContainSubstring("Closes #2"))
		})

		Context("post-review", Label("post-review"), func() {
			var pr *PostReviewResult

			BeforeAll(func() {
				var err error
				pr, err = RunPostReview(workDir)
				Expect(err).NotTo(HaveOccurred())
				if pr == nil {
					Skip("post-review script not found")
				}
			})

			It("should pass PR review", func() {
				Expect(pr.PRReview).To(Equal("ACCEPT"), "PR review found issues:\n%s", pr.Output)
			})

			It("should pass security review", func() {
				Expect(pr.Security).To(Equal("ACCEPT"), "Security review found issues:\n%s", pr.Output)
			})
		})
	})

	Describe("Issue 3: Logging Middleware (STANDARD)", Ordered, func() {
		var (
			workDir string
			result  *RunResult
		)

		BeforeAll(func() {
			dir, err := os.MkdirTemp("", "jorm-e2e-issue3-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)

			workDir, err = CloneCalibrationRepo(dir)
			Expect(err).NotTo(HaveOccurred())

			result, err = RunJorm(workDir, "3")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should exit successfully", func() {
			Expect(result.ExitCode).To(Equal(0), "jorm output:\n%s", result.Output)
		})

		It("should compile", func() {
			Expect(result.Compiles).To(BeTrue())
		})

		It("should pass all tests", func() {
			Expect(result.TestsPass).To(BeTrue())
		})

		It("should create the logging middleware", func() {
			Expect(HasFile(workDir, "internal/middleware/logging.go")).To(BeTrue())
		})

		It("should create middleware tests", func() {
			Expect(HasFile(workDir, "internal/middleware/logging_test.go")).To(BeTrue())
		})

		It("should include Closes #3 in the commit message", func() {
			msg := CommitMessage(workDir)
			Expect(msg).To(ContainSubstring("Closes #3"))
		})

		It("should wire middleware into main.go", func() {
			data, err := os.ReadFile(workDir + "/cmd/server/main.go")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Contains(string(data), "middleware") || strings.Contains(string(data), "Logging")).To(BeTrue(),
				"main.go should reference the logging middleware")
		})

		Context("post-review", Label("post-review"), func() {
			var pr *PostReviewResult

			BeforeAll(func() {
				var err error
				pr, err = RunPostReview(workDir)
				Expect(err).NotTo(HaveOccurred())
				if pr == nil {
					Skip("post-review script not found")
				}
			})

			It("should pass PR review", func() {
				Expect(pr.PRReview).To(Equal("ACCEPT"), "PR review found issues:\n%s", pr.Output)
			})

			It("should pass security review", func() {
				Expect(pr.Security).To(Equal("ACCEPT"), "Security review found issues:\n%s", pr.Output)
			})
		})
	})

	Describe("Issue 4: Cache Race Condition (DEBUG)", Ordered, func() {
		var (
			workDir string
			result  *RunResult
		)

		BeforeAll(func() {
			dir, err := os.MkdirTemp("", "jorm-e2e-issue4-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)

			workDir, err = CloneCalibrationRepo(dir)
			Expect(err).NotTo(HaveOccurred())

			result, err = RunJorm(workDir, "4")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should exit successfully", func() {
			Expect(result.ExitCode).To(Equal(0), "jorm output:\n%s", result.Output)
		})

		It("should compile", func() {
			Expect(result.Compiles).To(BeTrue())
		})

		It("should pass all tests", func() {
			Expect(result.TestsPass).To(BeTrue())
		})

		It("should pass the race detector", func() {
			Expect(TestsPassWithRace(workDir, "./internal/cache/...")).To(BeTrue(),
				"go test -race ./internal/cache/... should pass after fixing the race condition")
		})

		It("should include Closes #4 in the commit message", func() {
			msg := CommitMessage(workDir)
			Expect(msg).To(ContainSubstring("Closes #4"))
		})

		It("should add sync.RWMutex to Cache struct", func() {
			data, err := os.ReadFile(workDir + "/internal/cache/cache.go")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("sync.RWMutex"),
				"Cache struct should use sync.RWMutex for thread safety")
		})

		Context("post-review", Label("post-review"), func() {
			var pr *PostReviewResult

			BeforeAll(func() {
				var err error
				pr, err = RunPostReview(workDir)
				Expect(err).NotTo(HaveOccurred())
				if pr == nil {
					Skip("post-review script not found")
				}
			})

			It("should pass PR review", func() {
				Expect(pr.PRReview).To(Equal("ACCEPT"), "PR review found issues:\n%s", pr.Output)
			})

			It("should pass security review", func() {
				Expect(pr.Security).To(Equal("ACCEPT"), "Security review found issues:\n%s", pr.Output)
			})
		})
	})

	Describe("Issue 5: UpdateUser Endpoint (STANDARD)", Ordered, func() {
		var (
			workDir string
			result  *RunResult
		)

		BeforeAll(func() {
			dir, err := os.MkdirTemp("", "jorm-e2e-issue5-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)

			workDir, err = CloneCalibrationRepo(dir)
			Expect(err).NotTo(HaveOccurred())

			result, err = RunJorm(workDir, "5")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should exit successfully", func() {
			Expect(result.ExitCode).To(Equal(0), "jorm output:\n%s", result.Output)
		})

		It("should compile", func() {
			Expect(result.Compiles).To(BeTrue())
		})

		It("should pass all tests", func() {
			Expect(result.TestsPass).To(BeTrue())
		})

		It("should add Update method to Store interface", func() {
			data, err := os.ReadFile(workDir + "/internal/store/store.go")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("Update("),
				"Store interface should have an Update method")
		})

		It("should register PATCH /users/{id} route", func() {
			data, err := os.ReadFile(workDir + "/cmd/server/main.go")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("PATCH"),
				"main.go should register a PATCH route")
		})

		It("should include Closes #5 in the commit message", func() {
			msg := CommitMessage(workDir)
			Expect(msg).To(ContainSubstring("Closes #5"))
		})

		Context("post-review", Label("post-review"), func() {
			var pr *PostReviewResult

			BeforeAll(func() {
				var err error
				pr, err = RunPostReview(workDir)
				Expect(err).NotTo(HaveOccurred())
				if pr == nil {
					Skip("post-review script not found")
				}
			})

			It("should pass PR review", func() {
				Expect(pr.PRReview).To(Equal("ACCEPT"), "PR review found issues:\n%s", pr.Output)
			})

			It("should pass security review", func() {
				Expect(pr.Security).To(Equal("ACCEPT"), "Security review found issues:\n%s", pr.Output)
			})
		})
	})
})

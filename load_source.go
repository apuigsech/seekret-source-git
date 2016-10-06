package sourcegit

import (
	"fmt"
	"github.com/jasonmoo/ssh_config"
	"github.com/libgit2/git2go"
	"io/ioutil"
	"net/url"
	"os"
	"regexp"
	"github.com/apuigsech/seekret"
	"github.com/apuigsech/seekret/models"
)

var (
	SourceTypeGit = &SourceGit{}
)

type SourceGit struct{}

type SourceGitLoadOptions struct {
	Commit bool
	CommitCount int
	Staged bool
	CommitMessage: bool
}

func prepareGitLoadOptions(o seekret.LoadOptions) SourceGitLoadOptions {
	opt := SourceGitLoadOptions{
		Commit: true,
		CommitCount: 0,
		Staged: false,
		CommitMessage: false,
	}

	if commit, ok := o["commit"].(bool); ok {
		opt.Commit = commit
	}

	if commitCount, ok := o["commit-count"].(int); ok {
		opt.CommitCount = commitCount
	}

	if staged, ok := o["staged"].(bool); ok {
		opt.Staged = staged
	}

	if commitMessage, ok := o["commit-msg"].(bool); ok {
		opt.CommitMessage = commitMessage
	}

	return opt
}

func (s *SourceGit) LoadObjects(source string, o seekret.LoadOptions) ([]models.Object, error) {
	var objectList []models.Object
	opt := prepareGitLoadOptions(o)

	repo, err := openGitRepo(source)
	if err != nil {
		return nil, err
	}

	if opt.Commit {
		objectListCommit,err := objectsFromCommit(repo, opt.CommitCount)
		if err != nil {
			return nil,err
		}
		objectList = append(objectList, objectListCommit...)
	}

	if opt.Staged {
		objectListStaged,err := objectsFromStaged(repo)
		if err != nil {
			return nil,err
		}
		objectList = append(objectList, objectListStaged...)
	}

	if opt.CommitMessage {

	}

	return objectList, nil
}

func objectsFromCommit(repo *git.Repository, count int) ([]models.Object, error) {
	var objectList []models.Object

	walk, err := repo.Walk()
	if err != nil {
		return nil, err
	}

	if count > 0 {
		err := walk.PushRange(fmt.Sprintf("HEAD~%d..HEAD", count))
		if err != nil {
			err := walk.PushHead()
			if err != nil {
				return nil,err
			}
		}
	} else {
		err := walk.PushHead()
		if err != nil {
			return nil,err
		}
	}
	walk.Sorting(git.SortTime)

	err = walk.Iterate(func(commit *git.Commit) bool {
		tree, err := commit.Tree()
		if err != nil {
			fmt.Println(err)
		}

		// TODO: what to return?
		tree.Walk(func(base string, tentry *git.TreeEntry) int {
			if tentry.Type == git.ObjectBlob {
				blob, err := repo.LookupBlob(tentry.Id)
				if err != nil {
					return 0
				}

				o := models.NewObject(fmt.Sprintf("%s%s", base, tentry.Name),  blob.Contents())

				o.SetMetadata("commit", commit.Id().String(), models.MetadataAttributes{})
				o.SetMetadata("uniq-id", tentry.Id.String(), models.MetadataAttributes{
					PrimaryKey: true,
				})
				objectList = append(objectList, *o)
			}

			return 0
		})

		return true
	})

	if err != nil {
		return nil, err
	}

	return objectList, nil
}


func objectsFromStaged(repo *git.Repository) ([]models.Object, error) {
	var objectList []models.Object

	index, err := repo.Index()
	if err != nil {
		return nil,err
	}

	for i := 0; i < int(index.EntryCount()); i++ {

		entry, err := index.EntryByIndex(uint(i))
		if err != nil {
			return nil,err
		}

		status, err := repo.StatusFile(entry.Path)
		if err != nil {
			return nil,err
		}

		if status != git.StatusCurrent {
			blob, err := repo.LookupBlob(entry.Id)
			if err != nil {
				return nil,err
			}

			o := models.NewObject(entry.Path,  blob.Contents())

			// TODO: Type of staged.
			o.SetMetadata("status", "staged", models.MetadataAttributes{})
			objectList = append(objectList, *o)
		}
	}

	return objectList,nil
}

// Get the commit message from the current HEAD
func getCommitMessage(repo *git.Repository, count int) ([]models.Object, error) {

	var objectList []models.Object

	index, err := repo.Index()
	if err != nil {
		return nil,err
	}

	head, err := repo.Head()
	if err != nil {
		return nil,err
	}

	ref := head.Target()

	commit, err := repo.LookupCommit(ref)
	if err != nil {
		return nil,err
	}

	o := models.NewObject("COMMIT_EDITMSG", commit.Message())

	objectList = append(objectList, *o)

	return objectList,nil
}



func credentialsCallback(gitUri string, username string, allowedTypes git.CredType) (git.ErrorCode, *git.Cred) {
	sshConfigFile := os.ExpandEnv("$HOME/.ssh/config")

	fh, err := os.Open(sshConfigFile)
	if err != nil {
		panic(err)
	}

	c, err := ssh_config.Parse(fh)
	if err != nil {
		panic(err)
	}

	fh.Close()

	u, err := url.Parse(gitUri)
	if err != nil {
		panic(err)
	}

	host := c.FindByHostname(u.Host)
	idFile := host.GetParam("IdentityFile").Value()
	idFilePub := idFile + ".pub"

	ret, cred := git.NewCredSshKey("git", idFilePub, idFile, "")

	return git.ErrorCode(ret), &cred
}

func certificateCheckCallback(cert *git.Certificate, valid bool, hostname string) git.ErrorCode {
	return 0
}

func normalizeGitUri(source string) (string, bool) {
	var gitUri string

	gitregexp := regexp.MustCompile("^(?:(https?|git|ssh)://|(git@))([^:|/]+)(?:/|:)([^/]+)/([^/\\.]+)(.git)$")
	u := gitregexp.FindStringSubmatch(source)

	if len(u) == 0 {
		return source, false
	}

	var proto string
	if u[1] == "http" || u[1] == "https" || u[1] == "ssh" || u[1] == "git" {
		proto = u[1]
	} else {
		proto = "ssh"
	}

	gitUri = fmt.Sprintf("%s://%s%s/%s/%s%s", proto, u[2], u[3], u[4], u[5], u[6])

	return gitUri, true
}

func openGitRepo(source string) (*git.Repository, error) {
	var repo *git.Repository

	gitUri, remote := normalizeGitUri(source)

	if remote {
		return openGitRepoRemote(gitUri)
	} else {
		return openGitRepoLocal(source)
	}

	return repo, nil
}

func openGitRepoRemote(gitUri string) (*git.Repository, error) {
	var repo *git.Repository
	var err error

	tmpdir, err := ioutil.TempDir("", "seekret")
	if err != nil {
		return nil, err
	}

	repo, err = git.Clone(gitUri, tmpdir, &git.CloneOptions{
		FetchOptions: &git.FetchOptions{
			RemoteCallbacks: git.RemoteCallbacks{
				CredentialsCallback:      credentialsCallback,
				CertificateCheckCallback: certificateCheckCallback,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func openGitRepoLocal(source string) (*git.Repository, error) {
	repo, err := git.OpenRepositoryExtended(source, git.RepositoryOpenCrossFs, "")
	if  err != nil{
		return nil, err
	}

	return repo, nil
}

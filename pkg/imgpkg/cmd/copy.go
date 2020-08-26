package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/cppforlife/go-cli-ui/ui"
	regname "github.com/google/go-containerregistry/pkg/name"
	ctlimg "github.com/k14s/imgpkg/pkg/imgpkg/image"

	"github.com/spf13/cobra"
)

type CopyOptions struct {
	ui ui.UI

	RegistryFlags RegistryFlags
	Concurrency   int

	LockSrc   string
	TarSrc    string
	BundleSrc string
	ImageSrc  string

	RepoDst string
	TarDst  string
}

func NewCopyOptions(ui ui.UI) *CopyOptions {
	return &CopyOptions{ui: ui}
}

func NewCopyCmd(o *CopyOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "copy",
		Short:   "Copy a bundle from one location to another",
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: ``,
	}

	// TODO switch to using shared flags and collapse --images-lock into --lock
	cmd.Flags().StringVar(&o.LockSrc, "lock", "", "BundleLock of the bundle to relocate")
	cmd.Flags().StringVarP(&o.BundleSrc, "bundle", "b", "", "BundleLock of the bundle to relocate")
	cmd.Flags().StringVarP(&o.ImageSrc, "image", "i", "", "BundleLock of the bundle to relocate")
	cmd.Flags().StringVar(&o.RepoDst, "to-repo", "", "BundleLock of the bundle to relocate")
	cmd.Flags().StringVar(&o.TarDst, "to-tar", "", "BundleLock of the bundle to relocate")
	cmd.Flags().StringVar(&o.TarSrc, "from-tar", "", "BundleLock of the bundle to relocate")
	cmd.Flags().IntVar(&o.Concurrency, "concurrency", 5, "concurrency")
	return cmd
}

func (o *CopyOptions) Run() error {
	if !o.hasOneSrc() {
		return fmt.Errorf("Expected either --lock, --bundle (-b), --image (-i), or --tar as a source")
	}

	if !o.hasOneDest() {
		return fmt.Errorf("Expected either --to-tar or --to-repo")
	}

	if o.isTarSrc() && o.isTarDst() {
		return fmt.Errorf("Cannot use tar src with tar dst")
	}

	logger := ctlimg.NewLogger(os.Stderr)
	prefixedLogger := logger.NewPrefixedWriter("copy | ")
	registry := ctlimg.NewRegistry(o.RegistryFlags.AsRegistryOpts())
	imageSet := ImageSet{o.Concurrency, prefixedLogger}

	var importRepo regname.Repository
	var unprocessedImageUrls *UnprocessedImageURLs
	var err error
	var bundleURL string

	switch {
	case o.isTarSrc():
		importRepo, err = regname.NewRepository(o.RepoDst)
		if err != nil {
			return fmt.Errorf("Building import repository ref: %s", err)
		}
		tarImageSet := TarImageSet{imageSet, o.Concurrency, prefixedLogger}
		_, err = tarImageSet.Import(o.TarSrc, importRepo, registry)
	case o.isRepoSrc() && o.isTarDst():
		unprocessedImageUrls, bundleURL, err = o.GetUnprocessedImageURLs()
		if err != nil {
			return err
		}

		if bundleURL != "" {
			unprocessedImageUrls, err = checkBundleRepoForCollocatedImages(unprocessedImageUrls, bundleURL, registry)
		}

		tarImageSet := TarImageSet{imageSet, o.Concurrency, prefixedLogger}
		err = tarImageSet.Export(unprocessedImageUrls, o.TarDst, registry) // download to tar
	case o.isRepoSrc() && o.isRepoDst():
		unprocessedImageUrls, bundleURL, err = o.GetUnprocessedImageURLs()
		if err != nil {
			return err
		}

		if bundleURL != "" {
			unprocessedImageUrls, err = checkBundleRepoForCollocatedImages(unprocessedImageUrls, bundleURL, registry)
		}

		importRepo, err = regname.NewRepository(o.RepoDst)
		if err != nil {
			return fmt.Errorf("Building import repository ref: %s", err)
		}
		_, err = imageSet.Relocate(unprocessedImageUrls, importRepo, registry)
	}
	return err
}

func (o *CopyOptions) isTarSrc() bool {
	return o.TarSrc != ""
}

func (o *CopyOptions) isRepoSrc() bool {
	return o.ImageSrc != "" || o.BundleSrc != "" || o.LockSrc != ""
}

func (o *CopyOptions) isTarDst() bool {
	return o.TarDst != ""
}

func (o *CopyOptions) isRepoDst() bool {
	return o.RepoDst != ""
}

func (o *CopyOptions) hasOneDest() bool {
	repoSet := o.isRepoDst()
	tarSet := o.isTarDst()
	return (repoSet || tarSet) && !(repoSet && tarSet)
}

func (o *CopyOptions) hasOneSrc() bool {
	var seen bool
	for _, ref := range []string{o.LockSrc, o.TarSrc, o.BundleSrc, o.ImageSrc} {
		if ref != "" {
			if seen {
				return false
			}
			seen = true
		}
	}
	return seen
}

func (o *CopyOptions) GetUnprocessedImageURLs() (*UnprocessedImageURLs, string, error) {
	unprocessedImageURLs := NewUnprocessedImageURLs()
	var bundleRef string
	switch {

	case o.LockSrc != "":
		lock, err := ReadLockFile(o.LockSrc)
		if err != nil {
			return nil, "", err
		}
		switch {
		case lock.Kind == "BundleLock":
			bundleLock, err := ReadBundleLockFile(o.LockSrc)
			if err != nil {
				return nil, "", err
			}

			bundleRef = bundleLock.Spec.Image.DigestRef
			isBundle, err := isBundle(bundleRef, o.RegistryFlags.AsRegistryOpts())
			if err != nil {
				return nil, "", err
			}

			if !isBundle {
				return nil, "", fmt.Errorf("Expected image flag when given an image reference. Please run with -i instead of -b, or use -b with a bundle reference")
			}

			imageRefs, err := GetReferencedImages(bundleRef, o.RegistryFlags.AsRegistryOpts())
			if err != nil {
				return nil, "", err
			}

			for _, imgRef := range imageRefs {
				unprocessedImageURLs.Add(UnprocessedImageURL{imgRef})
			}
			//unprocessedImageURLs.Add(UnprocessedImageURL{bundleRef})

		case lock.Kind == "ImagesLock":
			imgLock, err := ReadImageLockFile(o.LockSrc)
			if err != nil {
				return nil, "", err
			}

			for _, img := range imgLock.Spec.Images {
				unprocessedImageURLs.Add(UnprocessedImageURL{img.DigestRef})
			}
		default:
			return nil, "", fmt.Errorf("Unexpected lock kind, expected bundleLock or imageLock, got: %v", lock.Kind)
		}

	case o.ImageSrc != "":
		isBundle, err := isBundle(o.ImageSrc, o.RegistryFlags.AsRegistryOpts())
		if err != nil {
			return nil, "", err
		}

		if isBundle {
			return nil, "", fmt.Errorf("Expected bundle flag when copying a bundle, please use -b instead of -i")
		}

		unprocessedImageURLs.Add(UnprocessedImageURL{URL: o.ImageSrc})

	default:
		bundleRef = o.BundleSrc

		isBundle, err := isBundle(bundleRef, o.RegistryFlags.AsRegistryOpts())
		if err != nil {
			return nil, "", err
		}

		if !isBundle {
			return nil, "", fmt.Errorf("Expected image flag when given an image reference. Please run with -i instead of -b, or use -b with a bundle reference")
		}

		imageRefs, err := GetReferencedImages(bundleRef, o.RegistryFlags.AsRegistryOpts())
		if err != nil {
			return nil, "", err
		}

		for _, imgRef := range imageRefs {
			unprocessedImageURLs.Add(UnprocessedImageURL{URL: imgRef})
		}
		//unprocessedImageURLs.Add(UnprocessedImageURL{URL: bundleRef})
	}

	return unprocessedImageURLs, bundleRef, nil
}

func checkBundleRepoForCollocatedImages(foundImages *UnprocessedImageURLs, bundleURL string, registry ctlimg.Registry) (*UnprocessedImageURLs, error) {
	checkedURLs := NewUnprocessedImageURLs()
	checkedURLs.Add(UnprocessedImageURL{bundleURL})

	bundleRepo := strings.Split(bundleURL, "@")[0]

	for _, img := range foundImages.All() {
		parts := strings.Split(img.URL, "@")
		if len(parts) != 2 {
			return nil, fmt.Errorf("Parsing image URL: %s", img.URL)
		}
		digest := parts[1]

		newURL := bundleRepo + "@" + digest
		ref, err := regname.NewDigest(newURL, regname.StrictValidation)
		if err != nil {
			return nil, err
		}

		_, err = registry.Generic(ref)
		if err == nil {
			checkedURLs.Add(UnprocessedImageURL{newURL})
		} else {
			checkedURLs.Add(img)
		}
	}

	return checkedURLs, nil
}
